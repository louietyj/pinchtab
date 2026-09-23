package external

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/pinchtab/pinchtab/internal/autosolver"
)

// arkoseCapture holds the per-session Arkose values grabbed by the bridge's
// document-start hook (window.__ptArkose).
type arkoseCapture struct {
	Blob string `json:"blob"`
	PK   string `json:"pk"`
	Surl string `json:"surl"`
}

// readArkoseCapture reads window.__ptArkose from the live page. Returns nil if
// the hook captured nothing (non-Arkose page, or blob not yet set).
func readArkoseCapture(ctx context.Context, executor autosolver.ActionExecutor) *arkoseCapture {
	var raw string
	expr := `(function(){try{return window.__ptArkose?JSON.stringify(window.__ptArkose):"";}catch(e){return "";}})()`
	if err := executor.Evaluate(ctx, expr, &raw); err != nil || raw == "" {
		return nil
	}
	var ac arkoseCapture
	if err := json.Unmarshal([]byte(raw), &ac); err != nil {
		return nil
	}
	return &ac
}

// readUserAgent reads navigator.userAgent from the live page; "" if unavailable.
func readUserAgent(ctx context.Context, executor autosolver.ActionExecutor) string {
	var ua string
	if err := executor.Evaluate(ctx, "navigator.userAgent", &ua); err != nil {
		return ""
	}
	return ua
}

// captchaWidget is the CAPTCHA embedded on a page: its type and the key to
// submit, read off the same element so the two cannot disagree.
type captchaWidget struct {
	typ string
	key string
}

// findCaptchaWidget locates the CAPTCHA embedded on the page. Every marker is an
// attribute, a class, or a resource URL, never a loose word: a page that merely
// links to another vendor would otherwise be misclassified, and CapSolver
// rejects a mismatched task type outright with ERROR_INVALID_TASK_DATA.
func findCaptchaWidget(html string) captchaWidget {
	// A key whose element names no vendor; the vendor must come from the scripts.
	unclassifiedKey := ""
	// A vendor whose element carries no key — enough to name the type, but not to
	// pair it with some other element's key.
	keylessVendor := ""

	for _, tag := range htmlTagRe.FindAllString(html, -1) {
		if pk := attrValue(tag, pkeyAttrRe); pk != "" {
			return captchaWidget{"funcaptcha", pk}
		}

		key := attrValue(tag, siteKeyAttrRe)
		// Explicit-render widgets are often marked by id rather than class —
		// Cloudflare's challenge page uses <div id="g-recaptcha">.
		vendor := vendorFromMarkers(attrValue(tag, classAttrRe) + " " + attrValue(tag, idAttrRe))

		switch {
		case vendor != "" && key != "":
			return captchaWidget{vendor, key}
		case vendor != "" && keylessVendor == "":
			keylessVendor = vendor
		case key != "" && unclassifiedKey == "":
			unclassifiedKey = key
		}
	}

	vendor := vendorFromResources(html)
	if vendor == "" {
		vendor = keylessVendor
	}
	if vendor == "" {
		return captchaWidget{}
	}

	switch vendor {
	case "funcaptcha":
		return captchaWidget{vendor, firstSubmatch(html, arkosePkURLRe, arkoseAPIPathRe, publicKeyJSONRe)}
	case "recaptcha":
		if unclassifiedKey == "" {
			// A rendered challenge frame means a real v2 widget exists even though
			// no element carries data-sitekey — the explicit-render path.
			if k := firstSubmatch(html, recaptchaFrameKeyRe); k != "" {
				return captchaWidget{vendor, k}
			}
			// No widget at all but api.js carries a render sitekey: that is v3.
			// render=explicit is v2's programmatic-render flag rather than a key.
			if m := recaptchaRenderRe.FindStringSubmatch(html); len(m) > 1 && !strings.EqualFold(m[1], "explicit") {
				return captchaWidget{"recaptcha-v3", m[1]}
			}
		}
	case "turnstile", "hcaptcha":
		if unclassifiedKey == "" {
			if k := firstSubmatch(html, frameSitekeyRe); k != "" {
				return captchaWidget{vendor, k}
			}
		}
	}
	return captchaWidget{vendor, unclassifiedKey}
}

// vendorFromMarkers names the vendor from one element's class and id tokens.
func vendorFromMarkers(markers string) string {
	switch lower := strings.ToLower(markers); {
	case strings.Contains(lower, "cf-turnstile"):
		return "turnstile"
	case strings.Contains(lower, "h-captcha"):
		return "hcaptcha"
	case strings.Contains(lower, "g-recaptcha"):
		return "recaptcha"
	}
	return ""
}

// vendorFromResources names the vendor from the scripts and frames the page
// loads, covering widgets rendered programmatically with no vendor class.
func vendorFromResources(html string) string {
	switch {
	case arkoseSrcRe.MatchString(html):
		return "funcaptcha"
	case turnstileSrcRe.MatchString(html):
		return "turnstile"
	case hcaptchaSrcRe.MatchString(html):
		return "hcaptcha"
	case recaptchaSrcRe.MatchString(html):
		return "recaptcha"
	}
	return ""
}

// attrValue reads one attribute out of a single tag, or "" when absent.
func attrValue(tag string, re *regexp.Regexp) string {
	if m := re.FindStringSubmatch(tag); len(m) > 1 {
		return m[1]
	}
	return ""
}

// firstSubmatch returns the first capture from the first regexp that matches.
func firstSubmatch(s string, res ...*regexp.Regexp) string {
	for _, re := range res {
		if m := re.FindStringSubmatch(s); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

// detectCaptchaType classifies the CAPTCHA on a page, or "" if none is
// recognized. reCAPTCHA splits into "recaptcha" (v2 checkbox/invisible) and
// "recaptcha-v3". Enterprise reCAPTCHA is not yet distinguished and takes the
// v2 path.
func detectCaptchaType(html string) string {
	return findCaptchaWidget(html).typ
}

var (
	// htmlTagRe yields one tag at a time so a marker and the key it belongs to
	// are read from the same element rather than from anywhere in the document.
	htmlTagRe   = regexp.MustCompile(`(?s)<[a-zA-Z][^>]*>`)
	classAttrRe = regexp.MustCompile(`(?i)\bclass\s*=\s*["']([^"']*)["']`)
	idAttrRe    = regexp.MustCompile(`(?i)\bid\s*=\s*["']([^"']*)["']`)

	// Explicit-render widgets carry no data-sitekey: the key rides the vendor's
	// own challenge frame URL, which is present once the widget has rendered.
	recaptchaFrameKeyRe = regexp.MustCompile(`(?i)/recaptcha/(?:api2|enterprise)/(?:anchor|bframe)\?[^"'>]*\bk=([A-Za-z0-9_-]{20,})`)
	frameSitekeyRe      = regexp.MustCompile(`(?i)[?&]sitekey=([A-Za-z0-9_-]{8,})`)

	// Vendor resource hosts. These identify the scripts and frames a page loads,
	// which is evidence of an embedded widget in a way a bare word is not.
	arkoseSrcRe    = regexp.MustCompile(`(?i)\b[a-z0-9-]*\.?arkoselabs\.com`)
	turnstileSrcRe = regexp.MustCompile(`(?i)\bchallenges\.cloudflare\.com`)
	hcaptchaSrcRe  = regexp.MustCompile(`(?i)\b(?:js\.|newassets\.)?hcaptcha\.com`)
	recaptchaSrcRe = regexp.MustCompile(`(?i)\b(?:www\.google\.com|www\.recaptcha\.net|recaptcha\.net)/recaptcha/`)

	pkeyAttrRe      = regexp.MustCompile(`(?i)data-pkey\s*=\s*["']([^"']+)["']`)
	publicKeyJSONRe = regexp.MustCompile(`(?i)"?public_?key"?\s*[:=]\s*["']([0-9A-Fa-f-]{20,})["']`)
	arkosePkURLRe   = regexp.MustCompile(`(?i)[?&]pk=([0-9A-Fa-f-]{20,})`)
	// The current embed carries the key only in the script path: /v2/<key>/api.js.
	arkoseAPIPathRe   = regexp.MustCompile(`(?i)arkoselabs\.com/v2/([0-9A-Fa-f-]{36})/api\.js`)
	arkoseSubdomainRe = regexp.MustCompile(`(?i)https?://([a-z0-9-]+\.arkoselabs\.com)`)
	// Case-insensitive so extraction matches detectCaptchaType (which lowercases);
	// tolerant of either quote style and whitespace around '='.
	siteKeyAttrRe = regexp.MustCompile(`(?i)data-sitekey\s*=\s*["']([^"']+)["']`)
	// reCAPTCHA v3: the sitekey rides the api.js script URL as ?…render=<key>.
	// Tolerates other query params before render= (e.g. ?onload=cb&render=key).
	recaptchaRenderRe = regexp.MustCompile(`(?i)recaptcha/(?:enterprise|api)\.js\?[^"'>]*\brender=([0-9A-Za-z_-]+)`)
	// reCAPTCHA v3 action: the value passed to grecaptcha.execute(key, {action:…}).
	recaptchaActionRe = regexp.MustCompile(`(?i)grecaptcha(?:\.enterprise)?\s*\.\s*execute\s*\(\s*["'][^"']*["']\s*,\s*\{[^}]*?action\s*:\s*["']([^"']+)["']`)
	// Fallback for the v3 action when it's declared as an HTML attribute.
	dataActionRe = regexp.MustCompile(`(?i)data-action\s*=\s*["']([^"']+)["']`)
)

// extractSitekey returns the key to submit for captchaType. It prefers the key
// found on the widget element itself, so a page hosting more than one vendor
// cannot pair one vendor's type with another's key.
func extractSitekey(html, captchaType string) string {
	if w := findCaptchaWidget(html); w.typ == captchaType && w.key != "" {
		return w.key
	}

	// The widget scan found nothing usable for this type — fall back to the
	// document-wide patterns, which is all a caller naming its own type can use.
	switch captchaType {
	case "funcaptcha":
		return firstSubmatch(html, pkeyAttrRe, arkosePkURLRe, arkoseAPIPathRe, publicKeyJSONRe)
	case "recaptcha-v3":
		// v3 has no data-sitekey widget; the key is in the api.js ?render= param.
		return firstSubmatch(html, recaptchaRenderRe)
	default:
		// reCAPTCHA v2 / hCaptcha / Turnstile all expose data-sitekey.
		return firstSubmatch(html, siteKeyAttrRe)
	}
}

// extractRecaptchaAction returns the action passed to grecaptcha.execute for a
// reCAPTCHA v3 page, or "" if none is found (the action is optional — CapSolver
// applies a default). Falls back to a data-action attribute.
func extractRecaptchaAction(html string) string {
	if m := recaptchaActionRe.FindStringSubmatch(html); len(m) > 1 {
		return m[1]
	}
	if m := dataActionRe.FindStringSubmatch(html); len(m) > 1 {
		return m[1]
	}
	return ""
}

// extractArkoseSubdomain finds the Arkose API JS host (e.g.
// client-api.arkoselabs.com, or a tenant subdomain like
// lnkd-api.arkoselabs.com) referenced by the page's enforcement script.
func extractArkoseSubdomain(html string) string {
	if m := arkoseSubdomainRe.FindStringSubmatch(html); len(m) > 1 {
		return "https://" + m[1]
	}
	return ""
}
