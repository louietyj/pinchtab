package observe

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/chromedp/cdproto/heapprofiler"
	"github.com/chromedp/cdproto/performance"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/pinchtab/pinchtab/internal/cdptk"
)

type HeapUsage struct {
	UsedJSHeapSize  int64 `json:"usedJSHeapSize"`
	TotalJSHeapSize int64 `json:"totalJSHeapSize"`
	JSHeapSizeLimit int64 `json:"jsHeapSizeLimit"`
	Documents       int   `json:"documents"`
	Nodes           int   `json:"nodes"`
	Listeners       int   `json:"listeners"`
	Frames          int   `json:"frames"`
	GC              bool  `json:"gc"`
}

const heapLimitExpression = `(() => { const m = globalThis.performance && performance.memory; return m ? m.jsHeapSizeLimit : 0; })()`

func ReadHeapUsage(ctx context.Context, collectGarbage bool) (*HeapUsage, error) {
	usage := &HeapUsage{GC: collectGarbage}
	if collectGarbage {
		if err := CollectGarbage(ctx); err != nil {
			return nil, err
		}
	}
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		used, total, _, _, err := runtime.GetHeapUsage().Do(ctx)
		if err != nil {
			return fmt.Errorf("runtime.getHeapUsage: %w", err)
		}
		usage.UsedJSHeapSize = int64(used)
		usage.TotalJSHeapSize = int64(total)

		if err := performance.Enable().Do(ctx); err != nil {
			return fmt.Errorf("performance.enable: %w", err)
		}
		metrics, err := performance.GetMetrics().Do(ctx)
		if err != nil {
			return fmt.Errorf("performance.getMetrics: %w", err)
		}
		for _, m := range metrics {
			switch m.Name {
			case "Documents":
				usage.Documents = int(m.Value)
			case "Nodes":
				usage.Nodes = int(m.Value)
			case "JSEventListeners":
				usage.Listeners = int(m.Value)
			case "Frames":
				usage.Frames = int(m.Value)
			}
		}
		return nil
	}))
	if err != nil {
		return nil, err
	}
	limit, err := readHeapLimit(ctx)
	if err != nil {
		return nil, err
	}
	usage.JSHeapSizeLimit = limit
	return usage, nil
}

func readHeapLimit(ctx context.Context) (int64, error) {
	contextID, err := cdptk.IsolatedContextID(ctx, "")
	if err != nil {
		return 0, fmt.Errorf("isolated context for heap limit: %w", err)
	}
	var limit int64
	err = chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		res, exc, err := runtime.Evaluate(heapLimitExpression).
			WithContextID(runtime.ExecutionContextID(contextID)).
			WithReturnByValue(true).
			Do(ctx)
		if err != nil {
			return err
		}
		if exc != nil {
			return fmt.Errorf("evaluate heap limit: %s", exc.Text)
		}
		if res == nil || len(res.Value) == 0 {
			return nil
		}
		var v float64
		if err := json.Unmarshal(res.Value, &v); err != nil {
			return fmt.Errorf("decode heap limit: %w", err)
		}
		limit = int64(v)
		return nil
	}))
	if err != nil {
		return 0, fmt.Errorf("read heap limit: %w", err)
	}
	return limit, nil
}

func CollectGarbage(ctx context.Context) error {
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		if err := heapprofiler.Enable().Do(ctx); err != nil {
			return fmt.Errorf("heapprofiler.enable: %w", err)
		}
		if err := heapprofiler.CollectGarbage().Do(ctx); err != nil {
			return fmt.Errorf("heapprofiler.collectGarbage: %w", err)
		}
		return nil
	}))
}

func TakeHeapSnapshot(ctx context.Context, write func(chunk string) error) error {
	if c := chromedp.FromContext(ctx); c == nil || c.Target == nil {
		return chromedp.ErrInvalidContext
	}
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	var mu sync.Mutex
	var writeErr error
	chromedp.ListenTarget(runCtx, func(ev any) {
		chunk, ok := ev.(*heapprofiler.EventAddHeapSnapshotChunk)
		if !ok {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if writeErr != nil {
			return
		}
		if err := write(chunk.Chunk); err != nil {
			writeErr = err
			cancelRun()
		}
	})

	runErr := chromedp.Run(runCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		if err := heapprofiler.Enable().Do(ctx); err != nil {
			return fmt.Errorf("heapprofiler.enable: %w", err)
		}
		if err := heapprofiler.TakeHeapSnapshot().WithReportProgress(false).Do(ctx); err != nil {
			return fmt.Errorf("heapprofiler.takeHeapSnapshot: %w", err)
		}
		return nil
	}))

	mu.Lock()
	defer mu.Unlock()
	if writeErr != nil {
		return writeErr
	}
	return runErr
}
