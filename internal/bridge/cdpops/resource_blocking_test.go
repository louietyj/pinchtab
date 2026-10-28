package cdpops

import (
	"reflect"
	"testing"
)

func TestAbsoluteURLPatterns(t *testing.T) {
	for glob, want := range map[string][]string{
		"*.jpg":                     {"*://*:*/*.jpg"},
		"*google-analytics.com/*":   {"*://*google-analytics.com:*/*"},
		"*fbcdn.net/*/fbevents.js":  {"*://*fbcdn.net:*/*/fbevents.js"},
		"*twitter.com/i/adsct":      {"*://*twitter.com:*/i/adsct"},
		"*/collect?*":               {"*://*:*/collect?*", "*://*:*/*/collect?*"},
		"*analytics.gif*":           {"*://*:*/*analytics.gif*"},
		"https://ads.example.com/*": {"https://ads.example.com/*"},
	} {
		if got := absoluteURLPatterns(glob); !reflect.DeepEqual(got, want) {
			t.Errorf("absoluteURLPatterns(%q) = %q, want %q", glob, got, want)
		}
	}
}

func TestEveryBlockListGlobBecomesAbsolute(t *testing.T) {
	for _, glob := range MediaBlockPatterns {
		for _, p := range absoluteURLPatterns(glob) {
			if len(p) < 6 || p[:6] != "*://*:" {
				t.Errorf("%q -> %q is not an absolute pattern", glob, p)
			}
		}
	}
}
