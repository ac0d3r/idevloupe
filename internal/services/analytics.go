package services

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"idevloupe/internal/lockdown"
)

// Crash report services. The mover moves logs, the analytics logs among
// them, to where crashreportcopymobile serves them over AFC, as Finder and
// iTunes do when they sync.
const (
	CrashReportMover = "com.apple.crashreportmover"
	CrashReportCopy  = "com.apple.crashreportcopymobile"
)

// AnalyticsBattery is the battery state iOS records in its daily analytics
// logs (Settings > Privacy & Security > Analytics & Improvements >
// Analytics Data), as "last_value_<Key>" fields.
type AnalyticsBattery struct {
	File   string
	Date   time.Time // the log's date, from its name or modification time
	Values map[string]int64
}

// AnalyticsScan records what the search for analytics logs saw, so that a
// report without them shows why.
type AnalyticsScan struct {
	MoveErr string
	// Listing is the entries of each directory read, up to a limit.
	Listing map[string][]string
	// Total is the number of entries of each directory read.
	Total map[string]int
	// Read is the logs read, newest first.
	Read []string
}

// ErrNoAnalytics means the device holds no analytics log at all: sharing
// iPhone analytics is off, or the device was erased or restored lately.
var ErrNoAnalytics = errors.New("the device holds no analytics logs")

// ErrNoBatteryInAnalytics means analytics logs exist but none read records
// the battery.
var ErrNoBatteryInAnalytics = errors.New("the analytics logs hold no battery records")

var (
	lastValueRe = regexp.MustCompile(`"?last_value_([A-Za-z0-9_]+)"?\s*[:=]\s*"?(-?\d+)`)
	// Battery-related analytics keys; others are not kept.
	batteryKeyRe = regexp.MustCompile(`(?i)capacity|cycle|battery|charge|fcc|qmax|health|auth|genuine|temperature|soc\b|ra\d`)
	logDateRe    = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})-(\d{6})`)
)

// ParseAnalyticsBattery extracts the battery fields of one analytics log.
// When a key occurs several times, the last occurrence wins, as the logs
// append newer records.
func ParseAnalyticsBattery(data []byte) map[string]int64 {
	vals := map[string]int64{}
	for _, m := range lastValueRe.FindAllSubmatch(data, -1) {
		key := string(m[1])
		if !batteryKeyRe.MatchString(key) {
			continue
		}
		if n, err := strconv.ParseInt(string(m[2]), 10, 64); err == nil {
			vals[key] = n
		}
	}
	return vals
}

func isAnalyticsLog(name string) bool {
	l := strings.ToLower(name)
	return (strings.Contains(l, "analytics") || strings.Contains(l, "log-aggregated")) &&
		!strings.HasSuffix(l, ".plist")
}

func logDate(name string, mtime time.Time) time.Time {
	if m := logDateRe.FindStringSubmatch(name); m != nil {
		if t, err := time.ParseInLocation("2006-01-02-150405", m[1]+"-"+m[2], time.Local); err == nil {
			return t
		}
	}
	return mtime
}

// moveCrashReports asks the mover to move the logs and waits for its "ping".
func moveCrashReports(l *lockdown.Client) error {
	c, err := l.OpenService(CrashReportMover)
	if err != nil {
		return err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(60 * time.Second))
	buf := make([]byte, 4)
	if _, err := io.ReadFull(c, buf); err != nil {
		return fmt.Errorf("crashreportmover: %w", err)
	}
	if string(buf) != "ping" {
		return fmt.Errorf("crashreportmover: unexpected reply %q", buf)
	}
	return nil
}

// ReadAnalyticsBattery finds the newest analytics log that records the
// battery and returns its battery fields.
func ReadAnalyticsBattery(l *lockdown.Client) (*AnalyticsBattery, *AnalyticsScan, error) {
	scan := &AnalyticsScan{Listing: map[string][]string{}, Total: map[string]int{}}
	// Moving is best effort: logs already moved are served anyway.
	moveErr := moveCrashReports(l)
	if moveErr != nil {
		scan.MoveErr = moveErr.Error()
	}

	c, err := l.OpenService(CrashReportCopy)
	if err != nil {
		if moveErr != nil {
			return nil, scan, fmt.Errorf("%w; %w", moveErr, err)
		}
		return nil, scan, err
	}
	afc := NewAFC(c)
	defer afc.Close()

	type cand struct {
		path string
		date time.Time
		size int64
	}
	var cands []cand
	for _, dir := range []string{"/", "/Analytics", "/Retired"} {
		names, err := afc.ReadDir(dir)
		if err != nil {
			continue
		}
		sort.Strings(names)
		scan.Total[dir] = len(names)
		scan.Listing[dir] = names[:min(len(names), 200)]
		for _, n := range names {
			if !isAnalyticsLog(n) {
				continue
			}
			p := strings.TrimSuffix(dir, "/") + "/" + n
			fi, err := afc.Stat(p)
			if err != nil || fi.IsDir {
				continue
			}
			cands = append(cands, cand{p, logDate(n, fi.MTime), fi.Size})
		}
	}
	if len(cands) == 0 {
		return nil, scan, ErrNoAnalytics
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].date.After(cands[j].date) })

	// Newest first; stop at the first log that records the battery. Bound
	// the work, as logs can be large and there can be many.
	const maxFiles, maxFileSize = 15, 32 << 20
	read := 0
	for _, cd := range cands {
		if read >= maxFiles {
			break
		}
		if cd.size > maxFileSize {
			continue
		}
		read++
		scan.Read = append(scan.Read, cd.path)
		data, err := afc.ReadFile(cd.path, maxFileSize)
		if err != nil {
			continue
		}
		vals := ParseAnalyticsBattery(data)
		_, hasHealth := vals["MaximumCapacityPercent"]
		_, hasCycles := vals["CycleCount"]
		if hasHealth || hasCycles {
			return &AnalyticsBattery{File: cd.path, Date: cd.date, Values: vals}, scan, nil
		}
	}
	return nil, scan, ErrNoBatteryInAnalytics
}
