package locales

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// sources returns the app's Go files that are not tests and not generated,
// from the module's root.
func sources(t *testing.T) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, dir := range []string{"../../cmd", "../../internal"} {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") ||
				strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, "_gen.go") {
				return err
			}
			b, err := os.ReadFile(path)
			out[path] = b
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// isCJK tells a Han character or a full-width punctuation mark.
func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || (r >= 0x3000 && r <= 0x303f) || (r >= 0xff00 && r <= 0xffef)
}

// The app says what the user reads through the catalogs: no Go string of
// it is written in Chinese, which a reader of another language would see.
func TestNoTextOutsideCatalogs(t *testing.T) {
	fset := token.NewFileSet()
	for path, src := range sources(t) {
		f, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, _ := strconv.Unquote(lit.Value)
			if strings.ContainsFunc(s, isCJK) {
				t.Errorf("%s: %s belongs in the catalogs", fset.Position(lit.Pos()), lit.Value)
			}
			return true
		})
	}
}

// Every message is said somewhere: one no code says is left over.
func TestEveryMessageUsed(t *testing.T) {
	b, err := os.ReadFile("en.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog map[string]any
	if err := json.Unmarshal(b, &catalog); err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, src := range sources(t) {
		all.Write(src)
	}
	code := all.String()
	for key := range catalog {
		if strings.HasPrefix(key, "@") {
			continue
		}
		name := strings.ToUpper(key[:1]) + key[1:]
		if !regexp.MustCompile(`\.` + name + `\b`).MatchString(code) {
			t.Errorf("message %q (%s) is never said", key, name)
		}
	}
}
