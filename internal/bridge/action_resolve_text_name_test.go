package bridge

import (
	"errors"
	"strings"
	"testing"

	"github.com/pinchtab/pinchtab/internal/selector"
)

// Each case is one a DOM-text matcher got wrong on a real site, or a rule the
// accessibility tree gives for free.
const textNameFixtureHTML = `<!doctype html>
<html><body>
<span id="quote">death is but the next great adventure</span>
<a id="next" href="#">Next <span aria-hidden="true">→</span></a>

<div style="display:none">Where from?</div>
<input id="from" role="combobox" placeholder="Where from?">

<p>Cheapest flights this week</p>
<div role="tablist">
  <div id="cheapest" role="tab">Cheapest</div>
  <div id="best" role="tab"><div>Best <button aria-label="Learn more about ranking">i</button></div></div>
</div>

<button id="close" aria-label="Close">×</button>
<button style="display:none">Delete</button>
<button id="deleteall">Delete all</button>
<button id="edit1">Edit</button><button id="edit2">Edit</button>
</body></html>`

func TestTextSelectorResolvesTheControlTheTextNames(t *testing.T) {
	ctx := newTextFixture(t, textNameFixtureHTML, "#next")

	for _, tc := range []struct {
		query, wantX, why string
	}{
		{"text:Next", "css:#next", "a link named Next, not the prose that mentions the word (quotes.toscrape.com/page/4/)"},
		{"text:Where from", "css:#from", "the input is named by its placeholder; the display:none copy is not in the tree (Google Flights)"},
		{"text:Cheapest", "css:#cheapest", "a role=tab is a control by its role, not by a list of ours"},
		{"text:Best", "css:#best", "the tab, not its info button, whose name is its own (Google Flights)"},
		{"text:Close", "css:#close", "an icon button is named by aria-label, whatever its glyph"},
		{"text:Delete", "css:#deleteall", "a hidden button is no answer, even an exact one"},
		{"text:great adventure", "css:#quote", "with no control named by it, plain text still answers"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			want := resolve(t, ctx, tc.wantX)
			if got := resolve(t, ctx, tc.query); got != want {
				t.Errorf("%s resolved to %s, want %s: %s", tc.query, describeNode(t, ctx, got), describeNode(t, ctx, want), tc.why)
			}
		})
	}
}

// Two misses from IKEA. A phrase that runs from plain text into a link is
// still the link; with two links in it, it is no one link. A space a
// downscaled screenshot lost is never matched around, only suggested.
func TestTextSelectorPhraseAcrossElementsAndNearMisses(t *testing.T) {
	ctx := newTextFixture(t, `<!doctype html><html><body>
<div>In stock in <a id="store" href="#">IKEA East Palo Alto, CA</a></div>
<div>Ships from <a href="#a">Oakland</a> or <a href="#b">Fresno</a></div>
<button id="more">+ 3 more</button>
</body></html>`, "#store")

	if got, want := resolve(t, ctx, "text:In stock in IKEA East Palo Alto, CA"), resolve(t, ctx, "css:#store"); got != want {
		t.Errorf("phrase resolved to %s, want the store link", describeNode(t, ctx, got))
	}
	if _, err := ResolveUnifiedSelector(ctx, selector.Parse("text:Ships from Oakland or Fresno"), nil); !errors.Is(err, ErrSelectorAmbiguous) {
		t.Errorf("a phrase holding two links: err = %v, want ambiguous", err)
	}
	_, err := ResolveUnifiedSelector(ctx, selector.Parse("text:+3 more"), nil)
	if !errors.Is(err, ErrSelectorNoMatch) || !strings.Contains(err.Error(), `did you mean button "+ 3 more"?`) {
		t.Errorf("text:+3 more: err = %v, want a no-match naming the button", err)
	}
}

// Two controls with the same name: neither is the obvious target, so the
// selector is refused rather than resolved to the first in the document. The
// positional wrappers the refusal suggests index exactly those controls.
func TestTextSelectorRefusesSameNamedControlsAndWrappersIndexThem(t *testing.T) {
	ctx := newTextFixture(t, textNameFixtureHTML, "#next")
	if _, err := ResolveTextToNodeID(ctx, "Edit"); !errors.Is(err, ErrSelectorAmbiguous) {
		t.Errorf("text:Edit over two Edit buttons: err = %v, want ErrSelectorAmbiguous", err)
	}
	for _, tc := range []struct{ query, wantX string }{
		{"first:text:Edit", "css:#edit1"},
		{"nth:1:text:Edit", "css:#edit2"},
		{"last:text:Edit", "css:#edit2"},
	} {
		want := resolve(t, ctx, tc.wantX)
		if got := resolve(t, ctx, tc.query); got != want {
			t.Errorf("%s resolved to %s, want %s", tc.query, describeNode(t, ctx, got), describeNode(t, ctx, want))
		}
	}
	// count text: counts the same candidates.
	for query, want := range map[string]int{"Edit": 2, "Next": 1, "nowhere on the page": 0} {
		if got, err := CountText(ctx, "", query); err != nil || got != want {
			t.Errorf("CountText(%q) = %d, %v; want %d", query, got, err, want)
		}
	}
}
