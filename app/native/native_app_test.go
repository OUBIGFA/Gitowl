package main

import (
	"errors"
	"testing"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
)

func TestNativeGenerationDisabledReasonsCoverItsStatusGates(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans"} {
		t.Run(locale, func(t *testing.T) {
			tx := locales.In(locale)
			a := newApp(nil, nil)
			defer a.close()
			a.core = &backend.Kernel{} // Only inspect the production presentation predicate; no RPC is called.
			a.ready, a.generationCapable = true, true
			a.included["fixture.txt"] = true
			for _, scenario := range []struct {
				set   func()
				reset func()
				want  string
			}{
				{func() { a.opening = true }, func() { a.opening = false }, tx.LoadingRepository()},
				{func() { a.loadingStatus = true }, func() { a.loadingStatus = false }, tx.LoadingStatus()},
				{func() { a.statusErr = errors.New("fixture status failure") }, func() { a.statusErr = nil }, tx.FailedStatus("fixture status failure")},
			} {
				scenario.set()
				if got := a.generationTooltip(tx); got != scenario.want {
					t.Fatalf("disabled reason lost: %q != %q", got, scenario.want)
				}
				scenario.reset()
			}
		})
	}
}
