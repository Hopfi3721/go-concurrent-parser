package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// Product — одна строка выгрузки.
type Product struct {
	Title  string
	Author string
	Link   string
}

// selectors описывает, где на странице искать данные.
// Благодаря этому парсер не привязан к одному сайту жёстко.
type selectors struct {
	Item   string
	Title  string
	Author string
	Link   string
}

type httpStatusError struct {
	Code       int
	RetryAfter time.Duration
}

// retryBackoff — стартовая пауза перед повтором. Переменная, а не константа,
// чтобы тесты не ждали секунды впустую.
var retryBackoff = time.Second

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("HTTP %d", e.Code)
}

// scrape обходит страницы в нескольких горутинах и возвращает то,
// что успели собрать. Отмена контекста (Ctrl+C) не теряет данные.
func scrape(ctx context.Context, cfg config) ([]Product, error) {
	client := &http.Client{Timeout: cfg.Timeout}

	results := make(chan Product, 100)
	var (
		collected []Product
		collector sync.WaitGroup
	)
	collector.Add(1)
	go func() {
		defer collector.Done()
		for item := range results {
			collected = append(collected, item)
		}
	}()

	var tick <-chan time.Time
	if cfg.Delay > 0 {
		ticker := time.NewTicker(cfg.Delay)
		defer ticker.Stop()
		tick = ticker.C
	}

	sem := make(chan struct{}, cfg.Concurrency)
	var wg sync.WaitGroup

	for page := 1; page <= cfg.Pages; page++ {
		select {
		case <-ctx.Done():
			wg.Wait()
			close(results)
			collector.Wait()
			return collected, ctx.Err()
		case sem <- struct{}{}:
		}

		wg.Add(1)
		go func(page int) {
			defer wg.Done()
			defer func() { <-sem }()

			if tick != nil {
				select {
				case <-ctx.Done():
					return
				case <-tick:
				}
			}

			pageURL := buildPageURL(cfg.BaseURL, cfg.PagePattern, page)
			items, err := fetchPage(ctx, client, pageURL, cfg.Selectors, cfg.Retries)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("[!] страница %d (%s): %v", page, pageURL, err)
				return
			}

			for _, item := range items {
				select {
				case results <- item:
				case <-ctx.Done():
					return
				}
			}
		}(page)
	}

	wg.Wait()
	close(results)
	collector.Wait()

	return collected, ctx.Err()
}

func buildPageURL(base, pattern string, page int) string {
	return strings.TrimRight(base, "/") + fmt.Sprintf(pattern, page)
}

// fetchPage повторяет запрос при сетевых ошибках, 429 и 5xx.
// 4xx (кроме 429) не повторяются: ответ не изменится.
func fetchPage(ctx context.Context, client *http.Client, pageURL string, sel selectors, retries int) ([]Product, error) {
	var lastErr error
	backoff := retryBackoff

	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
		}

		items, err := fetchOnce(ctx, client, pageURL, sel)
		if err == nil {
			return items, nil
		}
		lastErr = err

		var statusErr *httpStatusError
		if errors.As(err, &statusErr) {
			if statusErr.Code != http.StatusTooManyRequests && statusErr.Code < 500 {
				return nil, err
			}
			if statusErr.RetryAfter > 0 {
				backoff = min(statusErr.RetryAfter, 30*time.Second)
			}
		}
	}

	return nil, lastErr
}

func fetchOnce(ctx context.Context, client *http.Client, pageURL string, sel selectors) ([]Product, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &httpStatusError{
			Code:       resp.StatusCode,
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}

	return parsePage(pageURL, resp.Body, sel)
}

func parsePage(pageURL string, reader io.Reader, sel selectors) ([]Product, error) {
	doc, err := goquery.NewDocumentFromReader(reader)
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}

	base, err := url.Parse(pageURL)
	if err != nil {
		return nil, fmt.Errorf("parse page url: %w", err)
	}

	products := make([]Product, 0)
	doc.Find(sel.Item).Each(func(_ int, s *goquery.Selection) {
		title := strings.TrimSpace(s.Find(sel.Title).First().Text())
		if title == "" {
			return
		}

		author := strings.TrimSpace(s.Find(sel.Author).First().Text())

		link := ""
		if href, ok := s.Find(sel.Link).First().Attr("href"); ok {
			if absolute, err := base.Parse(href); err == nil {
				link = absolute.String()
			}
		}

		products = append(products, Product{
			Title:  title,
			Author: author,
			Link:   link,
		})
	})

	return products, nil
}

// parseRetryAfter понимает оба формата заголовка: секунды и HTTP-дату.
func parseRetryAfter(header string) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}

	if seconds, err := strconv.Atoi(header); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}

	if at, err := http.ParseTime(header); err == nil {
		if wait := time.Until(at); wait > 0 {
			return wait
		}
	}

	return 0
}
