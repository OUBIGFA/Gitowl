// Package i18n says ArcMD's interface in the language the reader picked:
// the interface is written in Chinese, and T looks each message up in the
// language shown, falling back to the Chinese where a translation is
// missing. It does not depend on the UI, so every package can use it.
package i18n

import (
	"strings"
	"sync/atomic"
)

// Languages are the languages the interface is written in, by BCP 47 tag.
var Languages = []string{"zh-Hans", "en"}

// current is the language shown, "zh-Hans" or "en".
var current atomic.Value

func init() { current.Store("zh-Hans") }

// SetLanguage shows the interface in tag: English for any "en" tag,
// Chinese otherwise.
func SetLanguage(tag string) {
	if strings.HasPrefix(strings.ToLower(tag), "en") {
		current.Store("en")
		return
	}
	current.Store("zh-Hans")
}

// Language is the language shown.
func Language() string { return current.Load().(string) }

// English reports whether the interface shows English.
func English() bool { return Language() == "en" }

// T says the message zh, written in Chinese, in the language shown.
func T(zh string) string {
	if Language() == "en" {
		if s, ok := en[zh]; ok {
			return s
		}
	}
	return zh
}

// Pick chooses the system's language among those the interface is written
// in: the first preferred one that is Chinese or English, else English.
func Pick(preferred []string) string {
	for _, p := range preferred {
		p = strings.ToLower(p)
		switch {
		case strings.HasPrefix(p, "zh"):
			return "zh-Hans"
		case strings.HasPrefix(p, "en"):
			return "en"
		}
	}
	return "en"
}
