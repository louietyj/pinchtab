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
