package handlers

import "testing"

func TestStuckRedirectTarget(t *testing.T) {
	stuck := "https://login.aliexpress.com/sync_cookie_read.htm?xman_goto=https%3A%2F%2Fwww.aliexpress.us%2Fw%2Fwholesale-usb-c-hub.html"
	for _, tc := range []struct{ landed, requested, want string }{
		{stuck, "https://www.aliexpress.us/w/wholesale-usb-c-hub.html", "https://www.aliexpress.us/w/wholesale-usb-c-hub.html"},
		// A hand-off to somewhere the caller did not ask for is not followed.
		{stuck, "https://www.example.com/", ""},
		{"https://www.aliexpress.us/w/wholesale-usb-c-hub.html", "https://www.aliexpress.us/w/wholesale-usb-c-hub.html", ""},
		{"https://login.aliexpress.com/sync_cookie_read.htm?other=1", "https://www.aliexpress.us/", ""},
	} {
		if got := stuckRedirectTarget(tc.landed, tc.requested); got != tc.want {
			t.Errorf("stuckRedirectTarget(%q, %q) = %q, want %q", tc.landed, tc.requested, got, tc.want)
		}
	}
}
