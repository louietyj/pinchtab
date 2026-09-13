package solvers

import (
	"math"
	"sync"
	"testing"

	"github.com/pinchtab/pinchtab/internal/srccensus"
)

func TestClickJitterStaysInsideItsWindow(t *testing.T) {
	limit := cfClickJitterPx / 2.0
	for i := 0; i < 1000; i++ {
		got := cfClickJitter()
		if math.Abs(got) > limit {
			t.Fatalf("cfClickJitter() = %v, outside ±%v: a click offset wider than the checkbox lands beside it", got, limit)
		}
	}
}

// Measured on egov.uscis.gov's managed challenge: the container spans the 896px
// content column and the checkbox is at x 233-257, y 325-349.
func TestCheckboxPointLandsOnTheCheckboxInAWideContainer(t *testing.T) {
	x, y := turnstileCheckboxPoint(&boundingBox{x: 224, y: 304, width: 896, height: 68.39})
	margin := cfClickJitterPx / 2.0
	if x-margin < 233 || x+margin > 257 {
		t.Errorf("checkbox x = %v ± %v, outside the checkbox at 233-257", x, margin)
	}
	if y-margin < 325 || y+margin > 349 {
		t.Errorf("checkbox y = %v ± %v, outside the checkbox at 325-349", y, margin)
	}
}

// A constant offset is not jitter: it clicks the same relative point every attempt,
// which is the fingerprint the jitter was ported to remove.
func TestClickJitterVaries(t *testing.T) {
	first := cfClickJitter()
	for i := 0; i < 1000; i++ {
		if cfClickJitter() != first {
			return
		}
	}
	t.Fatalf("cfClickJitter() returned %v on 1001 consecutive draws, so every attempt clicks the identical relative point", first)
}

// Solve runs inside HTTP handler goroutines and inside the auto-trigger's own
// goroutine, so two solves can draw at once. This test only FAILS under -race, which
// the standard unit run does not use — TestThisPackageDrawsRandomnessFromTheGlobalSource
// is the check that bites in a plain run, and this one is what a -race run reports.
func TestClickJitterIsSafeForConcurrentSolves(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				_ = cfClickJitter()
			}
		}()
	}
	wg.Wait()
}

// math/rand's *Rand is not goroutine-safe; the top-level functions are. A private
// seeded generator in this package would therefore be raced by concurrent solves, and
// the race is invisible to the standard unit run because it carries no -race flag —
// so the property is pinned at the source instead of by the concurrent test above.
func TestThisPackageDrawsRandomnessFromTheGlobalSource(t *testing.T) {
	pkg := srccensus.Load(t, ".", 2)

	for _, site := range pkg.CallsAllowingNone("rand.New") {
		t.Errorf("%s constructs its own *rand.Rand; math/rand's *Rand is not goroutine-safe and a solver draws from several goroutines at once (an HTTP handler and the auto-trigger). Call the top-level rand functions, which are, and keep the #nosec note on the call", site)
	}
}
