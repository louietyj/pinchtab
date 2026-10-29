package autosolver

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"regexp"
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
		{"cloudflare bot-management beacon", "Discord - Group Chat That's All Fun & Games", "https://discord.com/", `<script>var a=document.createElement('script');a.src='/cdn-cgi/challenge-platform/scripts/jsd/main.js';</script>`},
		{"vendor urls in a bundle", "", "https://auth.site.example/u/login", `<script>case I:return"https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit";case D:return"https://"+n+".arkoselabs.com/v2/"+r+"/api.js";case P:return"https://www.recaptcha.net/recaptcha/api.js?render=explicit&hl="+e</script><form><button>Continue</button></form>`},
		{"recaptcha v2 rendered explicitly", "Sign up", "https://site.example/signup", `<script src="https://www.google.com/recaptcha/api.js?render=explicit&onload=cb"></script>`},
	} {
		if intent := DetectChallengeIntent(tc.title, tc.url, tc.html); intent != nil {
			t.Errorf("%s: read as %s (%s)", tc.name, intent.ChallengeType, intent.Details)
		}
	}
}

// Real unchallenged pages, as cloak rendered them, behind vendors that leave
// scripts on every page. Styles, SVGs, JSON and data: URIs are emptied for size;
// scripts are whole.
func TestCapturedOrdinaryPagesAreNotChallenges(t *testing.T) {
	files, err := filepath.Glob("testdata/ordinary/*.html.gz")
	if err != nil || len(files) == 0 {
		t.Fatalf("no captured pages: %v", err)
	}
	header := regexp.MustCompile(`^<!-- url: (\S+) -->\n<!-- title: (.*) -->\n`)
	for _, f := range files {
		page := readGzip(t, f)
		m := header.FindStringSubmatch(page)
		if m == nil {
			t.Fatalf("%s: missing url/title header", f)
		}
		if intent := DetectChallengeIntent(m[2], m[1], page); intent != nil {
			t.Errorf("%s: read as %s (%s)", filepath.Base(f), intent.ChallengeType, intent.Details)
		}
	}
}

func readGzip(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	b, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return string(b)
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
		{"injected turnstile script", "Log in", "https://auth.site.example/u/login", `<script>var s=document.createElement("script")</script><script src="https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit&onload=cb"></script>`, "turnstile"},
		{"recaptcha v3 script", "Sign up", "https://site.example/signup", `<script src="https://www.google.com/recaptcha/api.js?render=6Le-wvkSAAAAAPBMRTvw0Q4Muexq9bi0DJwx_mJ-"></script>`, "recaptcha-v3"},
		{"arkose script", "Sign up", "https://site.example/signup", `<script src="https://client-api.arkoselabs.com/v2/11111111-1111-1111-1111-111111111111/api.js" data-callback="cb"></script>`, "funcaptcha"},
	} {
		intent := DetectChallengeIntent(tc.title, tc.url, tc.html)
		if intent == nil || intent.ChallengeType != tc.want {
			t.Errorf("%s: got %+v, want %s", tc.name, intent, tc.want)
		}
	}
}
