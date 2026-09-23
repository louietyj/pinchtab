package bridge

import (
	"context"
	"log/slog"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// geetestCaptureScript runs at document-start and wraps GeeTest's initGeetest
// (v3) and initGeetest4 (v4) as the vendor script defines them. It records the
// init config on window.__ptGeetest, the captcha object on window.__ptGeetestObj,
// and every handler the site registers through onSuccess on
// window.__ptGeetestSuccess.
//
// A solved GeeTest reaches the site only through that object: the site reads
// captchaObj.getValidate() from its onSuccess handler. The autosolver answers
// getValidate with the solve and calls the handlers. Without this hook a token
// can be bought but not delivered. It is inert on pages that never define
// initGeetest; the limitation noted on arkoseCaptureScript applies the same way.
const geetestCaptureScript = `(function(){
  if (window.__ptGeetestHook) return;
  try { Object.defineProperty(window, '__ptGeetestHook', {value:true, configurable:true}); } catch(e){ return; }
  function trap(name, version){
    var real;
    try{
      Object.defineProperty(window, name, {
        configurable: true, enumerable: true,
        get: function(){ return real; },
        set: function(v){
          if (typeof v !== 'function' || v.__ptWrapped){ real = v; return; }
          var wrapped = function(cfg, cb){
            try{
              window.__ptGeetest = { version: version, gt: cfg && cfg.gt || '', challenge: cfg && cfg.challenge || '',
                captchaId: cfg && (cfg.captchaId || cfg.captcha_id) || '' };
            }catch(e){}
            var args = Array.prototype.slice.call(arguments);
            args[1] = function(obj){
              try{
                window.__ptGeetestObj = obj;
                var handlers = window.__ptGeetestSuccess = [];
                if (obj && typeof obj.onSuccess === 'function'){
                  var on = obj.onSuccess;
                  obj.onSuccess = function(fn){ if (typeof fn === 'function') handlers.push(fn); return on.apply(this, arguments); };
                }
              }catch(e){}
              if (typeof cb === 'function') return cb.apply(this, arguments);
            };
            return v.apply(this, args);
          };
          wrapped.__ptWrapped = true;
          real = wrapped;
        }
      });
    }catch(e){}
  }
  trap('initGeetest', 3);
  trap('initGeetest4', 4);
})();`

// injectGeetestCapture installs geetestCaptureScript on the current target.
// Mirrors injectArkoseCapture; failures are non-fatal.
func (b *Bridge) injectGeetestCapture(ctx context.Context) {
	if err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(geetestCaptureScript).Do(ctx)
			return err
		}),
	); err != nil {
		slog.Warn("geetest capture injection failed", "err", err)
	}
}
