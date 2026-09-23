package autosolver

import "strings"

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

	if containsAny(lowerTitle, "captcha", "verify you are human", "i am not a robot") ||
		containsAny(lowerURL, "captcha", "recaptcha", "hcaptcha", "turnstile") ||
		containsAny(lowerHTML, "captcha", "verify you are human", "i am not a robot") {
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

func isRecaptchaV2Challenge(url, html string) bool {
	return containsAny(url,
		"recaptcha",
		"google.com/recaptcha",
	) || containsAny(html,
		"g-recaptcha",
		"recaptcha-checkbox",
		"api2/anchor",
		"google.com/recaptcha/api.js",
	)
}

func isHCaptchaChallenge(url, html string) bool {
	return containsAny(url,
		"hcaptcha",
	) || containsAny(html,
		"hcaptcha.com/1/api.js",
		"h-captcha",
		"hcaptcha",
	)
}

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
	htmlSignal := containsAny(html,
		"__cf_chl",
		"window._cf_chl_opt",
		"challenge-form",
		"jschl",
		"bot challenge",
		"anti-bot",
		"anti bot",
		"please enable javascript",
		"checking your browser before accessing",
		"browser integrity check",
		"navigator.webdriver",
	)

	// Primary signal comes from HTML markers.
	if htmlSignal {
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
