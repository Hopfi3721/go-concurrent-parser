package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const defaultBaseURL = "https://quotes.toscrape.com"

type config struct {
	BaseURL     string
	PagePattern string
	Pages       int
	OutputFile  string
	Concurrency int
	Delay       time.Duration
	Retries     int
	Timeout     time.Duration
	Selectors   selectors
}

func main() {
	baseURL := flag.String("url", defaultBaseURL, "Базовый URL сайта для парсинга")
	pagePattern := flag.String("page-pattern", "/page/%d/", "Шаблон пути страницы, %d — номер страницы")
	pages := flag.Int("pages", 5, "Количество страниц для обхода")
	out := flag.String("out", "output", "Имя итогового CSV файла (расширение .csv добавится автоматически)")
	concurrency := flag.Int("concurrency", 5, "Максимальное количество одновременных запросов")
	delayMs := flag.Int("delay", 500, "Минимальный интервал между запросами в мс (0 — без ограничения)")
	retries := flag.Int("retries", 3, "Повторы при сетевой ошибке, 429 и 5xx")
	timeout := flag.Duration("timeout", 10*time.Second, "Таймаут одного запроса")

	itemSelector := flag.String("item", ".quote", "CSS-селектор карточки на странице")
	titleSelector := flag.String("title", ".text", "CSS-селектор заголовка внутри карточки")
	authorSelector := flag.String("author", ".author", "CSS-селектор автора внутри карточки")
	linkSelector := flag.String("link", `a[href^='/author/']`, "CSS-селектор ссылки внутри карточки")

	flag.Parse()

	cfg := config{
		BaseURL:     *baseURL,
		PagePattern: *pagePattern,
		Pages:       *pages,
		OutputFile:  ensureCSVExtension(*out),
		Concurrency: *concurrency,
		Delay:       time.Duration(*delayMs) * time.Millisecond,
		Retries:     *retries,
		Timeout:     *timeout,
		Selectors: selectors{
			Item:   *itemSelector,
			Title:  *titleSelector,
			Author: *authorSelector,
			Link:   *linkSelector,
		},
	}

	if err := cfg.validate(); err != nil {
		log.Fatalf("неверные параметры: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if code := run(ctx, cfg); code != 0 {
		os.Exit(code)
	}
}

func (c config) validate() error {
	if c.BaseURL == "" {
		return fmt.Errorf("-url must not be empty")
	}
	if !strings.Contains(c.PagePattern, "%d") {
		return fmt.Errorf("-page-pattern must contain %%d")
	}
	if c.Pages <= 0 {
		return fmt.Errorf("-pages must be positive")
	}
	if c.Concurrency <= 0 {
		return fmt.Errorf("-concurrency must be positive")
	}
	if c.Delay < 0 {
		return fmt.Errorf("-delay must not be negative")
	}
	if c.Retries < 0 {
		return fmt.Errorf("-retries must not be negative")
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("-timeout must be positive")
	}

	return nil
}

func run(ctx context.Context, cfg config) int {
	start := time.Now()

	fmt.Println("==========================================")
	fmt.Printf("[+] Базовый URL:    %s\n", cfg.BaseURL)
	fmt.Printf("[+] Страниц:        %d\n", cfg.Pages)
	fmt.Printf("[+] Потоков:        %d\n", cfg.Concurrency)
	fmt.Printf("[+] Задержка:       %v\n", cfg.Delay)
	fmt.Printf("[+] Выходной файл:  %s\n", cfg.OutputFile)
	fmt.Println("==========================================")

	products, err := scrape(ctx, cfg)
	if err != nil && ctx.Err() == nil {
		log.Printf("[!] парсинг прерван: %v", err)
	}

	fmt.Println("------------------------------------------")
	fmt.Printf("[+] Парсинг завершён за %v. Собрано элементов: %d\n", time.Since(start).Round(time.Millisecond), len(products))

	if err := exportToCSV(cfg.OutputFile, products); err != nil {
		log.Printf("ошибка экспорта в CSV: %v", err)
		return 1
	}

	fmt.Printf("[+] Данные сохранены в файл: %s\n", cfg.OutputFile)

	if ctx.Err() != nil {
		fmt.Println("[!] Работа прервана пользователем, сохранено то, что успели собрать")
		return 130
	}

	return 0
}
