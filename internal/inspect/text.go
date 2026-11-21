package inspect

import (
	"errors"

	"github.com/ac0d3r/idevloupe/internal/locales"
	"github.com/ac0d3r/idevloupe/pkg/lockdown"
	"github.com/ac0d3r/idevloupe/pkg/services"
	"github.com/ac0d3r/idevloupe/pkg/usbmux"
)

func joinWith(join func(first, next string) string, parts ...string) string {
	out := ""
	for _, p := range parts {
		switch {
		case p == "":
		case out == "":
			out = p
		default:
			out = join(out, p)
		}
	}
	return out
}

func ErrorText(lang string, err error) string {
	m := locales.In(lang)
	var step *StepError
	switch {
	case errors.As(err, &step):
		return m.StepFailed(step.Step.Text(lang, step.Target), ErrorText(lang, step.Err))
	case errors.Is(err, usbmux.ErrNoDaemon):
		return m.WithNote(m.ErrorNoUsbmuxd(), err.Error())
	case errors.Is(err, lockdown.ErrPairingPending):
		return m.ErrorPairingPending()
	case errors.Is(err, lockdown.ErrPasswordLocked):
		return m.ErrorPasswordLocked()
	case errors.Is(err, lockdown.ErrUserDenied):
		return m.ErrorUserDenied()
	case errors.Is(err, services.ErrNoAnalytics):
		return m.NoAnalytics()
	case errors.Is(err, services.ErrNoBatteryInAnalytics):
		return m.NoBatteryInAnalytics()
	}
	return err.Error()
}
