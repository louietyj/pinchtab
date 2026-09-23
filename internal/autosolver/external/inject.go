package external

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/pinchtab/pinchtab/internal/autosolver"
)

// injectToken writes the solved token into the page's response field(s), makes
// the widget's own API return it, and fires the registered callback so the host
// page advances without a manual submit.
func injectToken(ctx context.Context, executor autosolver.ActionExecutor, captchaType string, sol tokenSolution) error {
	var js string
	switch captchaType {
	case "recaptcha":
		js = recaptchaInjectJS
	case "recaptcha-v3":
		js = recaptchaV3InjectJS
	case "hcaptcha":
		js = hcaptchaInjectJS
	case "turnstile":
		js = turnstileInjectJS
	case "funcaptcha":
		js = funcaptchaInjectJS
	case "mtcaptcha":
		js = mtcaptchaInjectJS
	default:
		return fmt.Errorf("no injector for captcha type %q", captchaType)
	}
	// The injector is an IIFE taking the token and respKey as arguments. Encode
	// them with json.Marshal — not %q — so they are valid JS string literals
	// (Go's %q does not escape U+2028/U+2029, which break a JS string).
	tokenLit, err := json.Marshal(sol.token())
	if err != nil {
		return fmt.Errorf("encode token for injection: %w", err)
	}
	respKeyLit, err := json.Marshal(sol.RespKey)
	if err != nil {
		return fmt.Errorf("encode respKey for injection: %w", err)
	}
	var ok bool
	expr := fmt.Sprintf("(%s)(%s,%s)", js, tokenLit, respKeyLit)
	return executor.Evaluate(ctx, expr, &ok)
}

const recaptchaInjectJS = `function(token){
  var els=document.querySelectorAll('textarea#g-recaptcha-response, textarea[name="g-recaptcha-response"]');
  if(!els.length){
    var ta=document.createElement('textarea');
    ta.id='g-recaptcha-response'; ta.name='g-recaptcha-response';
    ta.style.display='none'; document.body.appendChild(ta);
    els=[ta];
  }
  els.forEach(function(el){el.value=token;});
  try{
    var cfg=window.___grecaptcha_cfg;
    if(cfg&&cfg.clients){
      for(var cid in cfg.clients){
        var client=cfg.clients[cid];
        for(var k in client){
          var o=client[k];
          if(o&&typeof o==='object'){
            for(var kk in o){
              var oo=o[kk];
              if(oo&&typeof oo==='object'&&typeof oo.callback==='function'){
                try{oo.callback(token);}catch(e){}
              }
            }
          }
        }
      }
    }
  }catch(e){}
  return true;
}`

// injectAWSWAFCookie stores the solved aws-waf-token where the page's own SDK
// would, reloads, and checks the captcha is gone: AWS WAF reads the cookie on
// the next request, never from the page.
func injectAWSWAFCookie(ctx context.Context, executor autosolver.ActionExecutor, page autosolver.Page, token string, domains []string) error {
	pageURL := page.URL()
	u, err := url.Parse(pageURL)
	if err != nil {
		return fmt.Errorf("parse page URL: %w", err)
	}
	cookie := "aws-waf-token=" + token + "; path=/"
	// Written host-only when the SDK would scope it to a domain, a second cookie
	// of the same name would shadow the one the server reads.
	for _, d := range domains {
		if u.Hostname() == d || strings.HasSuffix(u.Hostname(), "."+d) {
			cookie += "; domain=" + d
			break
		}
	}
	if u.Scheme == "https" {
		cookie += "; secure"
	}
	lit, err := json.Marshal(cookie)
	if err != nil {
		return fmt.Errorf("encode cookie: %w", err)
	}
	var ok bool
	if err := executor.Evaluate(ctx, fmt.Sprintf("(function(c){document.cookie=c;return true;})(%s)", lit), &ok); err != nil {
		return fmt.Errorf("set aws-waf-token: %w", err)
	}
	if err := executor.Navigate(ctx, pageURL); err != nil {
		return fmt.Errorf("reload: %w", err)
	}
	html, err := page.HTML()
	if err != nil {
		return fmt.Errorf("read reloaded page: %w", err)
	}
	if awswafCaptchaSrcRe.MatchString(html) {
		return fmt.Errorf("aws-waf-token rejected: the reloaded page still serves the captcha")
	}
	return nil
}

// recaptchaV3InjectJS also answers grecaptcha.execute with the token: a v3 site
// reads it from the promise execute returns when the user acts, not from a field.
const recaptchaV3InjectJS = `function(token){
  try{
    var g=window.grecaptcha, solved=function(){return Promise.resolve(token);};
    if(g&&typeof g.execute==='function'){ g.execute=solved; }
    if(g&&g.enterprise&&typeof g.enterprise.execute==='function'){ g.enterprise.execute=solved; }
  }catch(e){}
  return (` + recaptchaInjectJS + `)(token);
}`

// fireDataCallbacksJS calls the global named by data-callback on each widget
// matching sel: sites that render declaratively learn of a solve only that way.
const fireDataCallbacksJS = `function fire(sel,token){
    document.querySelectorAll(sel).forEach(function(el){
      var cb=el.getAttribute('data-callback');
      try{ if(cb&&typeof window[cb]==='function'){ window[cb](token); } }catch(e){}
    });
  }`

// Sites read hCaptcha and Turnstile through getResponse as often as through the
// hidden field, and it answers from the widget's state, which injection never set.
const hcaptchaInjectJS = `function(token,respKey){
  ` + fireDataCallbacksJS + `
  document.querySelectorAll('textarea[name="h-captcha-response"], textarea[name="g-recaptcha-response"]').forEach(function(el){el.value=token;});
  try{ if(window.hcaptcha){ hcaptcha.getResponse=function(){return token;}; hcaptcha.getRespKey=function(){return respKey;}; } }catch(e){}
  fire('.h-captcha',token);
  return true;
}`

const turnstileInjectJS = `function(token){
  ` + fireDataCallbacksJS + `
  document.querySelectorAll('input[name="cf-turnstile-response"], textarea[name="cf-turnstile-response"]').forEach(function(el){el.value=token;});
  try{ if(window.turnstile){ turnstile.getResponse=function(){return token;}; } }catch(e){}
  fire('.cf-turnstile',token);
  return true;
}`

// mtcaptchaInjectJS fills the widget's hidden field, answers
// mtcaptcha.getVerifiedToken, and fires the configured verified-callback.
const mtcaptchaInjectJS = `function(token){
  document.querySelectorAll('input[name="mtcaptcha-verifiedtoken"]').forEach(function(el){el.value=token;});
  try{ if(window.mtcaptcha){ mtcaptcha.getVerifiedToken=function(){return token;}; } }catch(e){}
  try{
    var cb=window.mtcaptchaConfig&&window.mtcaptchaConfig['verified-callback'];
    if(typeof cb==='string'){ cb=window[cb]; }
    if(typeof cb==='function'){ cb({verifiedToken:token,isVerified:true}); }
  }catch(e){}
  return true;
}`

// funcaptchaInjectJS is best-effort: Arkose token consumption is
// integration-specific. We populate the common hidden token fields and
// emit the standard Arkose "challenge complete" postMessage that many
// enforcement embeds listen for. Sites with a custom callback may still
// require site-specific glue after the token is obtained.
const funcaptchaInjectJS = `function(token){
  var sel='input[name="fc-token"], input[name="verification-token"], input[name="arkose-token"], input[id*="arkose"], input[name*="captcha-token"], input[name="captchaResponse"]';
  document.querySelectorAll(sel).forEach(function(el){el.value=token;});
  // Preferred path: deliver the token through the site's own Arkose completion
  // callback, captured at document-start by the bridge hook.
  try{ if(typeof window.__ptArkoseOnCompleted==='function'){ window.__ptArkoseOnCompleted({token:token}); } }catch(e){}
  try{ window.postMessage(JSON.stringify({eventId:'challenge-complete',payload:{sessionToken:token}}),'*'); }catch(e){}
  return true;
}`
