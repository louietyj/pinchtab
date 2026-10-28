package bridge

import (
	"errors"
	"testing"
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
