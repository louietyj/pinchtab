package bridge

import (
	"context"
	"log/slog"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// vendorCaptureScript runs at document-start and records how a page wires up
// the captchas whose solves reach the site only through a callback: Tencent's
// `new TencentCaptcha(appId, callback, options)` and NetEase Yidun's
// `initNECaptcha({captchaId, onVerify, ...})`. The app/captcha ID and the
// callbacks land on window.__ptTencent and window.__ptYidun, where the
// autosolver reads the ID for the task and calls the callback with the answer.
// Inert on pages that never define either; the limitation noted on
// arkoseCaptureScript applies the same way.
const vendorCaptureScript = `(function(){
  if (window.__ptVendorHook) return;
  try { Object.defineProperty(window, '__ptVendorHook', {value:true, configurable:true}); } catch(e){ return; }
  // A Proxy, not a wrapper function: the vendors hang state off the function
  // itself (Yidun's loader reads initNECaptcha.ResourceLoader and .VERSION), and
  // a plain wrapper without them broke Yidun's widget silently.
  function trap(name, handler){
    var real;
    try{
      Object.defineProperty(window, name, {
        configurable: true, enumerable: true,
        get: function(){ return real; },
        set: function(v){ real = typeof v === 'function' ? new Proxy(v, handler) : v; }
      });
    }catch(e){}
  }
  trap('TencentCaptcha', {
    construct: function(target, args, newTarget){
      var inst = Reflect.construct(target, args, newTarget);
      try{ window.__ptTencent = { appId: String(args[0] || ''), callback: args[1], instance: inst }; }catch(e){}
      return inst;
    }
  });
  trap('initNECaptcha', {
    apply: function(target, self, args){
      var cfg = args[0];
      try{ window.__ptYidun = { captchaId: cfg && (cfg.captchaId || cfg.id) || '', onVerify: cfg && cfg.onVerify }; }catch(e){}
      return Reflect.apply(target, self, args);
    }
  });
})();`

// injectVendorCapture installs vendorCaptureScript on the current target.
// Mirrors injectArkoseCapture; failures are non-fatal.
func (b *Bridge) injectVendorCapture(ctx context.Context) {
	if err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(vendorCaptureScript).Do(ctx)
			return err
		}),
	); err != nil {
		slog.Warn("captcha vendor capture injection failed", "err", err)
	}
}
