package audit

import (
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
)

func TestPlanURLs(t *testing.T) {
	got := PlanURLs([]string{"a", "b", "a", "", "c", "b"})
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("PlanURLs = %v, want %v", got, want)
	}
	if got := PlanURLs(nil); len(got) != 0 {
		t.Errorf("PlanURLs(nil) = %v", got)
	}
}

func fakeAuditor() (PageAuditor, *[]string, *sync.Mutex) {
	var mu sync.Mutex
	var calls []string
	return func(url string, opts PageOptions) PageAudit {
		mu.Lock()
		calls = append(calls, url)
		mu.Unlock()
		return PageAudit{URL: url, BrowserPageData: BrowserPageData{AccessibilityScore: 100}}
	}, &calls, &mu
}

func TestRunAuditOrderingAndDedupe(t *testing.T) {
	auditor, calls, _ := fakeAuditor()
	report, err := RunAudit(
		AuditInput{URLs: []string{"http://x/home", "http://x/a", "http://x/home", "http://x/b"}},
		nil,
		RunOptions{Concurrency: 1},
		nil, auditor,
	)
	if err != nil {
		t.Fatalf("RunAudit: %v", err)
	}
	if report.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %q", report.SchemaVersion)
	}
	if (*calls)[0] != "http://x/home" {
		t.Errorf("entry URL not audited first: %v", *calls)
	}
	var urls []string
	for _, p := range report.Pages {
		urls = append(urls, p.URL)
	}
	want := []string{"http://x/home", "http://x/a", "http://x/b"}
	if !reflect.DeepEqual(urls, want) {
		t.Errorf("page order = %v, want %v", urls, want)
	}
	if report.SummaryScore != 100 {
		t.Errorf("SummaryScore = %d, want 100", report.SummaryScore)
	}
}

func TestRunAuditEntryFirstSynchronously(t *testing.T) {
	var entryDone atomic.Bool
	auditor := func(url string, opts PageOptions) PageAudit {
		if url == "http://x/entry" {
			entryDone.Store(true)
		} else if !entryDone.Load() {
			t.Errorf("page %s started before entry finished", url)
		}
		return PageAudit{URL: url}
	}
	if _, err := RunAudit(
		AuditInput{URLs: []string{"http://x/entry", "http://x/a", "http://x/b", "http://x/c"}},
		nil,
		RunOptions{Concurrency: 4},
		nil, auditor,
	); err != nil {
		t.Fatalf("RunAudit: %v", err)
	}
}

func TestRunAuditConcurrencyBound(t *testing.T) {
	var active, peak atomic.Int32
	auditor := func(url string, opts PageOptions) PageAudit {
		cur := active.Add(1)
		for {
			p := peak.Load()
			if cur <= p || peak.CompareAndSwap(p, cur) {
				break
			}
		}
		defer active.Add(-1)
		return PageAudit{URL: url}
	}
	var urls []string
	for i := range 20 {
		urls = append(urls, "http://x/p"+string(rune('a'+i)))
	}
	report, err := RunAudit(AuditInput{URLs: urls}, nil, RunOptions{Concurrency: 3}, nil, auditor)
	if err != nil {
		t.Fatalf("RunAudit: %v", err)
	}
	if len(report.Pages) != 20 {
		t.Errorf("pages = %d, want 20", len(report.Pages))
	}
	if peak.Load() > 3 {
		t.Errorf("peak concurrency = %d, want <= 3", peak.Load())
	}
	if report.Options.Concurrency != 3 {
		t.Errorf("Options.Concurrency = %d", report.Options.Concurrency)
	}
}

func TestRunAuditConcurrencyYieldsSameURLSet(t *testing.T) {
	urls := []string{"http://x/1", "http://x/2", "http://x/3", "http://x/4", "http://x/5"}
	pageSet := func(concurrency int) map[string]bool {
		auditor, _, _ := fakeAuditor()
		report, err := RunAudit(AuditInput{URLs: urls}, nil, RunOptions{Concurrency: concurrency}, nil, auditor)
		if err != nil {
			t.Fatalf("RunAudit(concurrency=%d): %v", concurrency, err)
		}
		set := map[string]bool{}
		for _, p := range report.Pages {
			set[p.URL] = true
		}
		return set
	}
	if !reflect.DeepEqual(pageSet(1), pageSet(3)) {
		t.Error("concurrency 1 and 3 yield different URL sets")
	}
}

func TestRunAuditSitemapMode(t *testing.T) {
	auditor, _, _ := fakeAuditor()
	discover := func(sitemapURL string) ([]string, error) {
		if sitemapURL != "http://x/sitemap.xml" {
			t.Errorf("sitemapURL = %q", sitemapURL)
		}
		return []string{"http://x/a", "http://x/b"}, nil
	}
	report, err := RunAudit(AuditInput{SitemapURL: "http://x/sitemap.xml"}, nil, RunOptions{}, discover, auditor)
	if err != nil {
		t.Fatalf("RunAudit: %v", err)
	}
	if len(report.Pages) != 2 {
		t.Errorf("pages = %d, want 2", len(report.Pages))
	}

	if _, err := RunAudit(AuditInput{SitemapURL: "http://x/sitemap.xml"}, nil, RunOptions{}, func(string) ([]string, error) {
		return nil, errors.New("fetch failed")
	}, auditor); err == nil {
		t.Error("RunAudit should surface sitemap fetch errors")
	}
}

func TestRunAuditSampleSizeAndEmpty(t *testing.T) {
	auditor, _, _ := fakeAuditor()
	report, err := RunAudit(
		AuditInput{URLs: []string{"http://x/p1.html", "http://x/p2.html", "http://x/p3.html"}},
		nil,
		RunOptions{SampleSize: 2},
		nil, auditor,
	)
	if err != nil {
		t.Fatalf("RunAudit: %v", err)
	}
	if len(report.Pages) != 2 {
		t.Errorf("pages = %d, want 2 (template group sampled)", len(report.Pages))
	}

	if _, err := RunAudit(AuditInput{}, nil, RunOptions{}, nil, auditor); err == nil {
		t.Error("RunAudit with no input should error")
	}
}

func TestRunAuditErrorsAreData(t *testing.T) {
	auditor := func(url string, opts PageOptions) PageAudit {
		if url == "http://x/down" {
			return NewPageAuditError(url, errors.New("connection refused"))
		}
		return PageAudit{URL: url, BrowserPageData: BrowserPageData{AccessibilityScore: 80}}
	}
	report, err := RunAudit(
		AuditInput{URLs: []string{"http://x/up", "http://x/down"}},
		nil,
		RunOptions{},
		nil, auditor,
	)
	if err != nil {
		t.Fatalf("RunAudit: %v", err)
	}
	if report.Pages[1].Error != "connection refused" {
		t.Errorf("failed page Error = %q", report.Pages[1].Error)
	}
	if report.SummaryScore != 80 {
		t.Errorf("SummaryScore = %d, want 80 (failed pages excluded)", report.SummaryScore)
	}
}

func TestPageStatus(t *testing.T) {
	cases := []struct {
		name string
		page PageResult
		want string
	}{
		{"clean", PageResult{}, "ok"},
		{"failed", PageResult{Error: "connection refused"}, "error: connection refused"},
		{
			"uncaught exception",
			PageResult{Browser: BrowserPageData{JSErrors: []JSError{{Message: "Uncaught: ReferenceError"}}}},
			"1 uncaught JS error(s)",
		},
		{
			"console error only",
			PageResult{Browser: BrowserPageData{ConsoleLogs: []ConsoleLogEntry{{Level: "error", Message: "boom"}}}},
			"ok",
		},
		{
			"failure outranks the exception",
			PageResult{Error: "timeout", Browser: BrowserPageData{JSErrors: []JSError{{Message: "Uncaught"}}}},
			"error: timeout",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PageStatus(tc.page); got != tc.want {
				t.Errorf("PageStatus = %q, want %q", got, tc.want)
			}
		})
	}
}

// ToPageResult populates the page's own StatusCode from the main document and
// removes that document from BrokenAssets — but a NESTED document (an iframe)
// that 404s is a real sub-resource and stays a broken asset.
func TestToPageResultDropsMainDocumentButKeepsAnIframeDocument(t *testing.T) {
	const page = "http://x/page.html"
	const iframe = "http://x/frame.html"
	pa := PageAudit{
		URL: page,
		BrowserPageData: BrowserPageData{
			NetworkRequests: []NetworkRequest{
				{URL: page, ResourceType: "Document", Status: 404, Failed: true},
				{URL: iframe, ResourceType: "Document", Status: 404, Failed: true},
			},
			BrokenAssets: []BrokenAsset{
				{URL: page, ResourceType: "document", Status: 404},
				{URL: iframe, ResourceType: "document", Status: 404},
			},
		},
	}

	pr := pa.ToPageResult()
	if pr.StatusCode != 404 {
		t.Errorf("StatusCode = %d, want 404 (the main document)", pr.StatusCode)
	}
	if len(pr.Browser.BrokenAssets) != 1 || pr.Browser.BrokenAssets[0].URL != iframe {
		t.Errorf("BrokenAssets = %+v, want only the iframe document kept", pr.Browser.BrokenAssets)
	}
	if got := PageStatus(pr); got != "HTTP 404" {
		t.Errorf("PageStatus = %q, want HTTP 404", got)
	}
}

// A 200 main document leaves the page ok and its status set, and does not touch
// sub-resource broken assets.
func TestToPageResultKeepsA200PageOk(t *testing.T) {
	const page = "http://x/ok.html"
	pa := PageAudit{
		URL: page,
		BrowserPageData: BrowserPageData{
			NetworkRequests: []NetworkRequest{{URL: page, ResourceType: "Document", Status: 200}},
			BrokenAssets:    []BrokenAsset{{URL: page + "?img", ResourceType: "image", Status: 404}},
		},
	}
	pr := pa.ToPageResult()
	if pr.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", pr.StatusCode)
	}
	if got := PageStatus(pr); got != "ok" {
		t.Errorf("PageStatus = %q, want ok", got)
	}
	if len(pr.Browser.BrokenAssets) != 1 {
		t.Errorf("a 200 page's sub-resource broken asset was dropped: %+v", pr.Browser.BrokenAssets)
	}
}
