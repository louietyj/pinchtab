package autosolver

import (
	"regexp"
	"strings"
)

var (
	punishFrameRe      = regexp.MustCompile(`<iframe[^>]+src="[^"]*/_____tmd_____/punish`)
	punishBlockFrameRe = regexp.MustCompile(`<iframe[^>]+src="[^"]*alicdn\.com/punish/punish:resource:template:`)
)

// childFrameIntent is a captcha widget found in a child frame (see
// DetectPageChallenge), named "<vendor>-in-frame".
func childFrameIntent(vendor string) *Intent {
	return &Intent{
		Type:          IntentCaptcha,
		Confidence:    0.9,
		ChallengeType: vendor + "-in-frame",
		Details:       vendor + " widget in a child frame",
	}
}

// DetectChallengeIntent classifies known challenge pages using title, URL,
// and HTML markers. It returns nil when no challenge signal is found.
func DetectChallengeIntent(title, url, html string) *Intent {
	lowerTitle := strings.ToLower(title)
	lowerURL := strings.ToLower(url)
	lowerHTML := strings.ToLower(html)
	markup := withoutScriptBodies(lowerHTML)
	v3 := isRecaptchaV3Challenge(lowerHTML, markup)

	if isTurnstileChallenge(lowerTitle, lowerURL, lowerHTML, markup) {
		return &Intent{
			Type:          IntentCaptcha,
			Confidence:    0.95,
			ChallengeType: "turnstile",
			Details:       "cloudflare turnstile challenge detected",
		}
	}

	// Alibaba's slide-to-end NoCaptcha. The punish interstitial mounts its
	// slider after load, so its URL is enough on its own.
	if strings.Contains(lowerURL, "/_____tmd_____/punish") || containsAny(lowerHTML, `id="nc_1_n1z"`, `id="nc_1_wrapper"`) {
		return &Intent{
			Type:          IntentCaptcha,
			Confidence:    0.9,
			ChallengeType: "nocaptcha",
			Details:       "Alibaba NoCaptcha slider detected",
		}
	}

	// The same punish page can instead come up in an iframe over an ordinary
	// page, when AliExpress punishes the page's data API rather than the page.
	// Its content is cross-origin, so the iframe itself is the only marker; while
	// it is there the challenge is not gone.
	// The outright block ("Sorry, there was a problem accessing the page") can
	// also arrive as an iframe of Alibaba's punish template over a blank page.
	// Nothing passes it, but it must not read as an ordinary page.
	if punishBlockFrameRe.MatchString(lowerHTML) {
		return &Intent{
			Type:          IntentCaptcha,
			Confidence:    0.9,
			ChallengeType: "punish-block",
			Details:       "Alibaba punish block in an iframe",
		}
	}

	if punishFrameRe.MatchString(lowerHTML) {
		return &Intent{
			Type:          IntentCaptcha,
			Confidence:    0.9,
			ChallengeType: "punish-frame",
			Details:       "Alibaba punish challenge in an iframe",
		}
	}

	if v3 {
		return &Intent{
			Type:          IntentCaptcha,
			Confidence:    0.9,
			ChallengeType: "recaptcha-v3",
			Details:       "reCAPTCHA v3 challenge detected",
		}
	}

	if !v3 && isRecaptchaV2Challenge(lowerURL, lowerHTML) {
		return &Intent{
			Type:          IntentCaptcha,
			Confidence:    0.9,
			ChallengeType: "recaptcha-v2",
			Details:       "reCAPTCHA v2 challenge detected",
		}
	}

	if isFunCaptchaChallenge(markup) {
		return &Intent{
			Type:          IntentCaptcha,
			Confidence:    0.9,
			ChallengeType: "funcaptcha",
			Details:       "Arkose Labs FunCaptcha challenge detected",
		}
	}

	// Only the captcha action; a silent challenge (token.awswaf.com alone) clears
	// in the browser and has nothing to solve.
	if containsAny(lowerHTML, ".captcha.awswaf.com/") {
		return &Intent{
			Type:          IntentCaptcha,
			Confidence:    0.9,
			ChallengeType: "awswaf",
			Details:       "AWS WAF captcha detected",
		}
	}

	if containsAny(lowerHTML, "api.geetest.com/", "static.geetest.com/", "gcaptcha4.geetest.com/") {
		return &Intent{
			Type:          IntentCaptcha,
			Confidence:    0.9,
			ChallengeType: "geetest",
			Details:       "GeeTest challenge detected",
		}
	}

	// Tencent and Yidun load their scripts on pages that never show a captcha, so
	// only the rendered widget counts.
	for _, v := range []struct {
		typ, details string
		markers      []string
	}{
		{"tencent", "Tencent captcha detected", []string{"tcaptcha_transform", "tcaptcha_iframe"}},
		{"yidun", "NetEase Yidun captcha detected", []string{`class="yidun`}},
		{"lemin", "Lemin captcha detected", []string{"leminnow.com/captcha/"}},
		{"yandex", "Yandex SmartCaptcha detected", []string{"smartcaptcha.yandexcloud.net/"}},
		{"prosopo", "Prosopo Procaptcha detected", []string{"js.prosopo.io/", `class="procaptcha`}},
	} {
		if containsAny(lowerHTML, v.markers...) {
			return &Intent{Type: IntentCaptcha, Confidence: 0.9, ChallengeType: v.typ, Details: v.details}
		}
	}

	if containsAny(lowerHTML, "mtcaptcha.com/mtcv1/", `class="mtcaptcha"`) {
		return &Intent{
			Type:          IntentCaptcha,
			Confidence:    0.9,
			ChallengeType: "mtcaptcha",
			Details:       "MTCaptcha challenge detected",
		}
	}

	if isHCaptchaChallenge(lowerURL, lowerHTML) {
		return &Intent{
			Type:          IntentCaptcha,
			Confidence:    0.9,
			ChallengeType: "hcaptcha",
			Details:       "hCaptcha challenge detected",
		}
	}

	if isCustomJSChallenge(lowerTitle, lowerURL, lowerHTML) {
		return &Intent{
			Type:          IntentBlocked,
			Confidence:    0.85,
			ChallengeType: "custom-js",
			Details:       "custom JavaScript anti-bot challenge detected",
		}
	}

	if isGenericCaptcha(lowerTitle, lowerURL, lowerHTML) {
		return &Intent{
			Type:          IntentCaptcha,
			Confidence:    0.7,
			ChallengeType: "captcha-generic",
			Details:       "generic captcha challenge detected",
		}
	}

	return nil
}

// isGenericCaptcha is the last rung: no vendor was identified, so all that is left
// is the word itself. It applies the same rule the vendor checks above do — the
// title and the URL are guesses, and a guess is only worth making when there is no
// page to check it against.
//
// Every branch used to be sufficient on its own, and the URL branch listed
// "turnstile", which is a word about stadiums as often as about Cloudflare. An
// operations report at /ops/turnstile-report, or an article at
// /blog/how-recaptcha-works, was a captcha challenge as far as this was concerned.
//
// With the page in hand, a title asking the visitor something counts by itself.
// Otherwise the page must have little text, as challenge pages do, and either ask
// the same in its body or carry "captcha" as the title's own word ("Captcha
// Interception", not "How reCAPTCHA works").
func isGenericCaptcha(title, url, html string) bool {
	if html == "" {
		return containsAny(title, "captcha", "verify you are human", "i am not a robot") ||
			containsAny(url, "captcha", "turnstile")
	}
	if containsAny(title, "verify you are human", "i am not a robot") {
		return true
	}
	return visibleTextLen(html) < challengePageMaxText &&
		(captchaWordRe.MatchString(title) ||
			containsAny(html, "verify you are human", "i am not a robot", "check if you are a robot"))
}

var captchaWordRe = regexp.MustCompile(`\bcaptcha\b`)

// withoutScriptBodies empties inline scripts, keeping their tags. A vendor URL in
// script text is not a resource the page loaded: Auth0's login bundle names the
// api.js of Turnstile, Arkose and reCAPTCHA and loads none unless the tenant
// enables one. The HTML is the live DOM, so an injected script has its own tag.
func withoutScriptBodies(html string) string {
	return scriptBodyRe.ReplaceAllString(html, "$1</script>")
}

var scriptBodyRe = regexp.MustCompile(`(?is)(<script\b[^>]*>).*?</script>`)

// cfChallengeTokenRe finds a Cloudflare challenge token (__cf_chl_tk,
// __cf_chl_f_tk, ...) in a query string of the page's own: the interstitial's
// form action and the URL it restores. A page reached through a challenge
// carries the same token percent-encoded in its trackers' referrer (musescore's
// search results, right after the interstitial was solved), and that page is
// not a challenge.
var cfChallengeTokenRe = regexp.MustCompile(`[?&;]__cf_chl_`)

func isTurnstileChallenge(title, url, html, markup string) bool {
	if containsAny(url,
		"cdn-cgi/challenge-platform",
		"/cdn-cgi/challenge",
	) || containsAny(markup,
		"challenges.cloudflare.com/turnstile",
		"cf-turnstile",
	) || containsAny(html,
		"turnstile.render(",
		// An interstitial is served AT the address that was asked for, so the
		// challenge-platform script shows up in the markup while the URL still
		// looks like the site. Matching it only in the URL meant the real page
		// was recognised by its title and not by the thing running it.
		// Only /h/: bot management puts .../scripts/jsd/main.js on every page.
		"cdn-cgi/challenge-platform/h/",
		"cf-browser-verification",
	) || cfChallengeTokenRe.MatchString(html) {
		return true
	}

	// The title is a guess, and it is only worth making when there is no page to
	// check it against. "Attention required" and "just a moment" are ordinary
	// English: a legal notice headed "Attention Required Before You Sign", or a
	// support page saying "Just a moment while we finish setup", was being called
	// a Cloudflare interstitial at 0.95 confidence and sent to the solver instead
	// of being read.
	//
	// An interstitial that reached us WITH its markup always carries a marker
	// above — the challenge-platform script is what runs it. So once html is in
	// hand, a bare title match is a coincidence. With no html there is nothing to
	// corroborate against and the title is all the caller has, which is the mode
	// heuristics.go asks for by passing url and html empty.
	if html == "" {
		return containsAny(title,
			"just a moment",
			"attention required",
			"checking your browser",
		)
	}
	return false
}

func isRecaptchaV3Challenge(html, markup string) bool {
	return containsAny(html,
		"grecaptcha.execute(",
		"grecaptcha.enterprise.execute(",
	) || recaptchaV3ScriptRe.MatchString(markup)
}

// v3 loads api.js with its site key as render=; render=explicit is v2 drawing
// its checkbox on demand.
var recaptchaV3ScriptRe = regexp.MustCompile(`recaptcha/(?:api|enterprise)\.js\?render=[^&"'\s]{20,}`)

// challengePageMaxText separates a challenge page from an ordinary one that
// merely talks about captchas: a challenge is a widget and a sentence or two.
// Visible text, not HTML size: AliExpress's punish page is 114 KB of script.
const challengePageMaxText = 3000

var (
	scriptOrStyleRe = regexp.MustCompile(`(?is)<(script|style|noscript)\b.*?</(script|style|noscript)>`)
	tagRe           = regexp.MustCompile(`(?s)<[^>]*>`)
)

// visibleTextLen approximates how much text a page shows.
func visibleTextLen(html string) int {
	text := tagRe.ReplaceAllString(scriptOrStyleRe.ReplaceAllString(html, " "), " ")
	return len(strings.Join(strings.Fields(text), " "))
}

// The vendor detectors key on the widget's own resources and markup: a page
// can name the vendor ("/wiki/ReCAPTCHA", a link to hcaptcha.com) without
// showing its widget.
func isRecaptchaV2Challenge(url, html string) bool {
	return containsAny(url, "google.com/recaptcha") || recaptchaWidgetRe.MatchString(html)
}

func isHCaptchaChallenge(url, html string) bool {
	return containsAny(url, "hcaptcha.com/") || hcaptchaWidgetRe.MatchString(html)
}

// The widget markers count only in real attributes. An article's code sample
// shows the same words escaped (class=&quot;g-recaptcha&quot;) or as text.
// The vendor's api.js is not a widget: Epic Games Store loads hCaptcha's on every
// page and renders nothing.
var (
	recaptchaWidgetRe = regexp.MustCompile(`class=["'][^"']*\bg-recaptcha\b|src=["'][^"']*(?:google\.com|recaptcha\.net)/recaptcha/(?:api2/anchor|enterprise/anchor)`)
	hcaptchaWidgetRe  = regexp.MustCompile(`class=["'][^"']*\bh-captcha\b|src=["'][^"']*hcaptcha\.com/captcha/`)
)

// isFunCaptchaChallenge keys on Arkose's resource paths and widget attribute,
// never the bare vendor name: pages that sell solving mention it everywhere.
func isFunCaptchaChallenge(html string) bool {
	return containsAny(html,
		".arkoselabs.com/v2/",
		".arkoselabs.com/fc/",
		".arkoselabs.com/cdn/fc/",
		"data-pkey=",
	)
}

func isCustomJSChallenge(title, url, html string) bool {
	titleSignal := containsAny(title,
		"please enable javascript",
		"browser integrity check",
		"access denied",
		"forbidden",
		"blocked",
	)
	urlSignal := containsAny(url,
		"challenge",
		"bot",
		"verify",
	)
	// Cloudflare's own challenge markup is enough by itself.
	if cfChallengeTokenRe.MatchString(html) || containsAny(html,
		"window._cf_chl_opt",
		"challenge-form",
		"jschl",
		"checking your browser before accessing",
		"browser integrity check",
	) {
		return true
	}
	// These phrases fill ordinary pages too: nearly every site has a <noscript>
	// "please enable javascript", and fingerprinting scripts read
	// navigator.webdriver. With a block-page title they mean a challenge; alone
	// they sent jschallenge clicking submit buttons on ordinary pages.
	if titleSignal && containsAny(html,
		"bot challenge",
		"anti-bot",
		"anti bot",
		"please enable javascript",
		"navigator.webdriver",
	) {
		return true
	}

	// Fallback when HTML cannot be read.
	if html == "" && titleSignal && urlSignal {
		return true
	}

	return false
}

func containsAny(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(haystack, needle) {
			return true
		}
	}
	return false
}
