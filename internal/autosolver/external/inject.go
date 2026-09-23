package external

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pinchtab/pinchtab/internal/autosolver"
)

// injectToken writes the solved token into the page's response field(s)
// and, for reCAPTCHA, fires the registered callback so the host page
// advances without a manual submit.
func injectToken(ctx context.Context, executor autosolver.ActionExecutor, captchaType, token string) error {
	var js string
	switch captchaType {
	case "recaptcha", "recaptcha-v3":
		js = recaptchaInjectJS
	case "hcaptcha":
		js = hcaptchaInjectJS
	case "turnstile":
		js = turnstileInjectJS
	case "funcaptcha":
		js = funcaptchaInjectJS
	default:
		return fmt.Errorf("no injector for captcha type %q", captchaType)
	}
	// The injector is an IIFE taking the token as its argument. Encode the
	// token with json.Marshal — not %q — so it is a valid JS string literal
	// (Go's %q does not escape U+2028/U+2029, which break a JS string).
	tokenLit, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("encode token for injection: %w", err)
	}
	var ok bool
	expr := fmt.Sprintf("(%s)(%s)", js, tokenLit)
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

const hcaptchaInjectJS = `function(token){
  document.querySelectorAll('textarea[name="h-captcha-response"], textarea[name="g-recaptcha-response"]').forEach(function(el){el.value=token;});
  return true;
}`

const turnstileInjectJS = `function(token){
  document.querySelectorAll('input[name="cf-turnstile-response"], textarea[name="cf-turnstile-response"]').forEach(function(el){el.value=token;});
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
