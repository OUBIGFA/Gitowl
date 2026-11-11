package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The provider timeout uses the original MyGo number control. Its nested
// screen-reader actions must follow the interface language as well.
func TestNativeNumberActionsFollowInterfaceLanguage(t *testing.T) {
	for _, tc := range []struct{ locale, increase, decrease string }{
		{"en", "Increase", "Decrease"},
		{"zh-Hans", "增加", "减少"},
		{"zh-Hant", "增加", "減少"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			value := 60.0
			tt := ui.NewTester(func(c *ui.Context) {
				ui.NumberInput(c, &value, 0.1, 2147483.647, 0.1).Label("Timeout")
			}, 400, 100)
			tt.SetLocale(tc.locale)
			tt.Frame()
			if err := tt.Click(tc.increase); err != nil {
				t.Fatal(err)
			}
			if value != 60.1 {
				t.Fatalf("localized action changed numeric behavior: %v", value)
			}
			if err := tt.Click(tc.decrease); err != nil {
				t.Fatal(err)
			}
			if value != 60 {
				t.Fatalf("localized action changed numeric behavior: %v", value)
			}
		})
	}
}
