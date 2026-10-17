package cdpops

import (
	"context"

	"github.com/chromedp/cdproto/page"
)

func SetPageFrozen(ctx context.Context, frozen bool) error {
	state := page.SetWebLifecycleStateStateActive
	if frozen {
		state = page.SetWebLifecycleStateStateFrozen
	}
	return page.SetWebLifecycleState(state).Do(ctx)
}
