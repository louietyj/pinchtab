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
	v3 := isRecaptchaV3Challenge(lowerURL, lowerHTML)

	if isTurnstileChallenge(lowerTitle, lowerURL, lowerHTML) {
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

	if isFunCaptchaChallenge(lowerHTML) {
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

	// Words alone are what an article about captchas is made of ("CAPTCHA -
	// Wikipedia"). Only a title that asks the visitor something counts by
	// itself; the rest needs a page with little text, as challenge pages are.
	if containsAny(lowerTitle, "verify you are human", "i am not a robot") ||
		((containsAny(lowerTitle, "captcha") || containsAny(lowerURL, "captcha") ||
			containsAny(lowerHTML, "verify you are human", "i am not a robot", "check if you are a robot")) &&
			visibleTextLen(html) < challengePageMaxText) {
		return &Intent{
			Type:          IntentCaptcha,
			Confidence:    0.7,
			ChallengeType: "captcha-generic",
			Details:       "generic captcha challenge detected",
		}
	}

	return nil
}

func isTurnstileChallenge(title, url, html string) bool {
	return containsAny(title,
		"just a moment",
		"attention required",
		"checking your browser",
	) || containsAny(url,
		"cdn-cgi/challenge-platform",
		"/cdn-cgi/challenge",
	) || containsAny(html,
		"challenges.cloudflare.com/turnstile",
		"cf-turnstile",
		"turnstile.render(",
	)
}

func isRecaptchaV3Challenge(url, html string) bool {
	return containsAny(html,
		"grecaptcha.execute(",
		"grecaptcha.enterprise.execute(",
		"recaptcha/api.js?render=",
		"recaptcha/enterprise.js?render=",
	)
}

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
var (
	recaptchaWidgetRe = regexp.MustCompile(`class=["'][^"']*\bg-recaptcha\b|src=["'][^"']*(?:google\.com|recaptcha\.net)/recaptcha/(?:api\.js|enterprise\.js|api2/anchor|enterprise/anchor)`)
	hcaptchaWidgetRe  = regexp.MustCompile(`class=["'][^"']*\bh-captcha\b|src=["'][^"']*hcaptcha\.com/(?:1/api\.js|captcha/)`)
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
	if containsAny(html,
		"__cf_chl",
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
