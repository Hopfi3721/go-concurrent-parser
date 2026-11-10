package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const fixtureHTML = `<!DOCTYPE html>
<html>
<body>
  <div class="quote">
    <span class="text">“The world as we have created it is a process of our thinking.”</span>
    <span>by <small class="author">Albert Einstein</small>
    <a href="/author/Albert-Einstein">(about)</a></span>
  </div>
  <div class="quote">
    <span class="text">“It is our choices, Harry, that show what we truly are.”</span>
    <span>by <small class="author">J.K. Rowling</small>
    <a href="/author/J-K-Rowling">(about)</a></span>
  </div>
  <div class="quote">
    <span>Карточка без заголовка</span>
  </div>
</body>
</html>`

var testSelectors = selectors{
	Item:   ".quote",
	Title:  ".text",
	Author: ".author",
	Link:   `a[href^='/author/']`,
}

func testConfig(baseURL string) config {
	return config{
		BaseURL:     baseURL,
		PagePattern: "/page/%d/",
		Pages:       1,
		OutputFile:  "output.csv",
		Concurrency: 2,
		Delay:       0,
		Retries:     0,
		Timeout:     2 * time.Second,
		Selectors:   testSelectors,
	}
}

func TestParsePage(t *testing.T) {
	products, err := parsePage("https://quotes.toscrape.com/page/1/", strings.NewReader(fixtureHTML), testSelectors)
	if err != nil {
		t.Fatalf("parsePage: %v", err)
	}

	if len(products) != 2 {
		t.Fatalf("len(products) = %d, want 2 (карточка без заголовка должна быть пропущена)", len(products))
	}

	if !strings.Contains(products[0].Title, "The world as we have created it") {
		t.Errorf("Title = %q", products[0].Title)
	}
	if products[0].Author != "Albert Einstein" {
		t.Errorf("Author = %q, want Albert Einstein", products[0].Author)
	}
	if want := "https://quotes.toscrape.com/author/Albert-Einstein"; products[0].Link != want {
		t.Errorf("Link = %q, want %q", products[0].Link, want)
	}
}

func TestParsePageResolvesRelativeLinks(t *testing.T) {
	products, err := parsePage("https://example.com/catalog/page/3/", strings.NewReader(fixtureHTML), testSelectors)
	if err != nil {
		t.Fatalf("parsePage: %v", err)
	}
	if len(products) == 0 {
		t.Fatal("no products parsed")
	}

	if !strings.HasPrefix(products[0].Link, "https://example.com/") {
		t.Errorf("Link = %q, want absolute URL on https://example.com", products[0].Link)
	}
}

func TestBuildPageURL(t *testing.T) {
	tests := []struct {
		base    string
		pattern string
		page    int
		want    string
	}{
		{base: "https://quotes.toscrape.com", pattern: "/page/%d/", page: 2, want: "https://quotes.toscrape.com/page/2/"},
		{base: "https://quotes.toscrape.com/", pattern: "/page/%d/", page: 1, want: "https://quotes.toscrape.com/page/1/"},
		{base: "https://shop.ru/catalog", pattern: "?page=%d", page: 5, want: "https://shop.ru/catalog?page=5"},
	}

	for _, tt := range tests {
		if got := buildPageURL(tt.base, tt.pattern, tt.page); got != tt.want {
			t.Errorf("buildPageURL(%q, %q, %d) = %q, want %q", tt.base, tt.pattern, tt.page, got, tt.want)
		}
	}
}

func TestFetchOnceOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ua := r.Header.Get("User-Agent"); ua == "" {
			t.Error("request has no User-Agent")
		}
		fmt.Fprint(w, fixtureHTML)
	}))
	defer srv.Close()

	products, err := fetchOnce(context.Background(), srv.Client(), srv.URL, testSelectors)
	if err != nil {
		t.Fatalf("fetchOnce: %v", err)
	}
	if len(products) != 2 {
		t.Fatalf("len(products) = %d, want 2", len(products))
	}
}

func TestFetchOnceStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := fetchOnce(context.Background(), srv.Client(), srv.URL, testSelectors)

	var statusErr *httpStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("error = %v, want *httpStatusError", err)
	}
	if statusErr.Code != http.StatusTooManyRequests {
		t.Errorf("Code = %d, want 429", statusErr.Code)
	}
	if statusErr.RetryAfter != 3*time.Second {
		t.Errorf("RetryAfter = %v, want 3s", statusErr.RetryAfter)
	}
}

func TestFetchPageRetriesOnServerError(t *testing.T) {
	withFastBackoff(t)

	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, fixtureHTML)
	}))
	defer srv.Close()

	products, err := fetchPage(context.Background(), srv.Client(), srv.URL, testSelectors, 2)
	if err != nil {
		t.Fatalf("fetchPage: %v", err)
	}
	if len(products) != 2 {
		t.Fatalf("len(products) = %d, want 2", len(products))
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}

func TestFetchPageDoesNotRetryClientError(t *testing.T) {
	withFastBackoff(t)

	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if _, err := fetchPage(context.Background(), srv.Client(), srv.URL, testSelectors, 3); err == nil {
		t.Fatal("expected error for 404")
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("attempts = %d, want 1 — 404 повторять бессмысленно", got)
	}
}

func TestFetchPageRespectsCancelledContext(t *testing.T) {
	withFastBackoff(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fetchPage(ctx, srv.Client(), srv.URL, testSelectors, 3)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestScrapePages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page/1/", "/page/2/":
			fmt.Fprint(w, fixtureHTML)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := testConfig(srv.URL)
	cfg.Pages = 2

	products, err := scrape(context.Background(), cfg)
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	if len(products) != 4 {
		t.Fatalf("len(products) = %d, want 4", len(products))
	}
}

func TestScrapeSkipsBrokenPages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/page/2/" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, fixtureHTML)
	}))
	defer srv.Close()

	cfg := testConfig(srv.URL)
	cfg.Pages = 2

	products, err := scrape(context.Background(), cfg)
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	if len(products) != 2 {
		t.Fatalf("len(products) = %d, want 2 — одна битая страница не должна ронять весь парсинг", len(products))
	}
}

func TestScrapeCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()

	cfg := testConfig(srv.URL)
	cfg.Pages = 5
	cfg.Concurrency = 5

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	if _, err := scrape(ctx, cfg); err == nil {
		t.Fatal("expected context error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("scrape took %v after cancellation, want < 1s", elapsed)
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		header string
		want   time.Duration
	}{
		{header: "", want: 0},
		{header: "5", want: 5 * time.Second},
		{header: "0", want: 0},
		{header: "-3", want: 0},
		{header: "not-a-number", want: 0},
	}

	for _, tt := range tests {
		if got := parseRetryAfter(tt.header); got != tt.want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.header, got, tt.want)
		}
	}
}

func TestConfigValidate(t *testing.T) {
	valid := testConfig("https://example.com")
	if err := valid.validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*config)
	}{
		{name: "empty url", mutate: func(c *config) { c.BaseURL = "" }},
		{name: "pattern without placeholder", mutate: func(c *config) { c.PagePattern = "/page/" }},
		{name: "zero pages", mutate: func(c *config) { c.Pages = 0 }},
		{name: "zero concurrency", mutate: func(c *config) { c.Concurrency = 0 }},
		{name: "negative delay", mutate: func(c *config) { c.Delay = -time.Second }},
		{name: "negative retries", mutate: func(c *config) { c.Retries = -1 }},
		{name: "zero timeout", mutate: func(c *config) { c.Timeout = 0 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			tt.mutate(&cfg)
			if err := cfg.validate(); err == nil {
				t.Error("expected validation error, got nil")
			}
		})
	}
}

func withFastBackoff(t *testing.T) {
	t.Helper()

	original := retryBackoff
	retryBackoff = time.Millisecond
	t.Cleanup(func() { retryBackoff = original })
}
