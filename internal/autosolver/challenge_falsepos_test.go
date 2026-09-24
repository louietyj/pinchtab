package autosolver

import (
	"strings"
	"testing"
)

// Pages that talk about captchas, or carry the boilerplate every site has, are
// not challenges. Treating them as one cost seconds per nav, printed a false
// "NOT solved" HINT, and for custom-js sent jschallenge clicking submit buttons.
func TestOrdinaryPagesAreNotChallenges(t *testing.T) {
	article := strings.Repeat(`<p>A CAPTCHA asks users to prove they are human. reCAPTCHA, hCaptcha and Cloudflare Turnstile are common; a checkbox reads "I am not a robot" and some pages say "verify you are human". See <a href="https://www.hcaptcha.com/">hCaptcha</a> and <a href="https://www.google.com/recaptcha/about/">reCAPTCHA</a>.</p>`, 400)
	for _, tc := range []struct{ name, title, url, html string }{
		{"wikipedia CAPTCHA", "CAPTCHA - Wikipedia", "https://en.wikipedia.org/wiki/CAPTCHA_(disambiguation)", article},
		{"wikipedia reCAPTCHA", "reCAPTCHA - Wikipedia", "https://en.wikipedia.org/wiki/ReCAPTCHA", article},
		{"code sample in an article", "reCAPTCHA - Wikipedia", "https://en.wikipedia.org/wiki/ReCAPTCHA", `<pre>&lt;div class=&quot;g-recaptcha&quot; data-sitekey=&quot;your_site_key&quot;&gt;&lt;/div&gt; loads https://www.google.com/recaptcha/api.js and renders api2/anchor</pre>` + article},
		{"noscript boilerplate", "Roblox", "https://www.roblox.com/login", `<noscript>Please enable JavaScript to use this site.</noscript><script>if(navigator.webdriver){report('anti-bot')}</script><form><button type="submit">Log In</button></form>`},
	} {
		if intent := DetectChallengeIntent(tc.title, tc.url, tc.html); intent != nil {
			t.Errorf("%s: read as %s (%s)", tc.name, intent.ChallengeType, intent.Details)
		}
	}
}

func TestRealChallengePagesAreStillDetected(t *testing.T) {
	punish := `<html><head><title>Captcha Interception</title><script>` + strings.Repeat("var _x=1;", 12000) + `</script></head><body><div id="nocaptcha"></div><p>Please slide to verify</p></body></html>`
	for _, tc := range []struct{ name, title, url, html, want string }{
		{"script-heavy challenge page", "Captcha Interception", "https://site.example/verify", punish, "captcha-generic"},
		{"cloudflare js challenge", "Just a moment...", "https://site.example/", `<form id="challenge-form" action="/?__cf_chl_f_tk=x">`, "turnstile"},
		{"cloudflare markup without the title", "site", "https://site.example/", `<script>window._cf_chl_opt={cvId:'3'}</script>`, "custom-js"},
		{"block page with noscript", "Access Denied", "https://site.example/", `<noscript>Please enable JavaScript</noscript>`, "custom-js"},
		{"small verify page", "site", "https://site.example/check", `<h1>Please verify you are human</h1><div id="w"></div>`, "captcha-generic"},
		{"captcha title", "Security CAPTCHA", "https://site.example/", `<div></div>`, "captcha-generic"},
		{"hcaptcha widget", "Sign up", "https://site.example/signup", `<div class="h-captcha" data-sitekey="a5f74b19-9e45-40e0-b45d-47ff91b7a6c2"></div><script src="https://js.hcaptcha.com/1/api.js"></script>`, "hcaptcha"},
		{"recaptcha widget", "Sign up", "https://site.example/signup", `<div class="g-recaptcha" data-sitekey="6Le-wvkSAAAAAPBMRTvw0Q4Muexq9bi0DJwx_mJ-"></div>`, "recaptcha-v2"},
		{"rendered recaptcha frame", "Sign up", "https://site.example/signup", `<div id="w"><iframe title="reCAPTCHA" src="https://www.google.com/recaptcha/api2/anchor?ar=1&k=6Le-wvkSAAAAAPBMRTvw0Q4Muexq9bi0DJwx_mJ-"></iframe></div>`, "recaptcha-v2"},
		{"hcaptcha frame", "Sign up", "https://site.example/signup", `<iframe src="https://newassets.hcaptcha.com/captcha/v1/abc/static/hcaptcha.html#frame=checkbox"></iframe>`, "hcaptcha"},
	} {
		intent := DetectChallengeIntent(tc.title, tc.url, tc.html)
		if intent == nil || intent.ChallengeType != tc.want {
			t.Errorf("%s: got %+v, want %s", tc.name, intent, tc.want)
		}
	}
}
