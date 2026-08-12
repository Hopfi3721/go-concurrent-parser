package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type Product struct {
	Title  string
	Author string
	Link   string
}

func ensureCSVExtension(filename string) string {
	ext := filepath.Ext(filename)
	if ext == "" {
		return filename + ".csv"
	}
	if ext != ".csv" {
		return strings.TrimSuffix(filename, ext) + ".csv"
	}
	return filename
}

func main() {
	baseURL := flag.String("url", "https://quotes.toscrape.com", "Базовый URL для парсинга")
	maxPages := flag.Int("pages", 5, "Количество страниц для обхода")
	outputFileFlag := flag.String("out", "output", "Имя итогового CSV файла (расширение .csv добавится автоматически)")
	maxConcurrency := flag.Int("concurrency", 5, "Максимальное количество параллельных потоков")
	delayMs := flag.Int("delay", 500, "Задержка перед каждым запросом в миллисекундах")

	flag.Parse()

	// Гарантируем расширение .csv
	outputFile := ensureCSVExtension(*outputFileFlag)

	start := time.Now()

	fmt.Println("==========================================")
	fmt.Printf("[+] Базовый URL:    %s\n", *baseURL)
	fmt.Printf("[+] Страниц:        %d\n", *maxPages)
	fmt.Printf("[+] Потоков:        %d\n", *maxConcurrency)
	fmt.Printf("[+] Задержка:       %d ms\n", *delayMs)
	fmt.Printf("[+] Выходной файл:  %s\n", outputFile)
	fmt.Println("==========================================")

	results := make(chan Product, 100)
	semaphore := make(chan struct{}, *maxConcurrency)
	var wg sync.WaitGroup

	var products []Product
	var collectorWg sync.WaitGroup
	collectorWg.Add(1)
	go func() {
		defer collectorWg.Done()
		for prod := range results {
			products = append(products, prod)
		}
	}()

	for i := 1; i <= *maxPages; i++ {
		pageURL := fmt.Sprintf("%s/page/%d/", strings.TrimRight(*baseURL, "/"), i)
		wg.Add(1)
		semaphore <- struct{}{}

		go func(url string, pageNum int) {
			defer wg.Done()
			defer func() { <-semaphore }()

			if *delayMs > 0 {
				time.Sleep(time.Duration(*delayMs) * time.Millisecond)
			}

			fmt.Printf(" [->] [Стр. %d/%d] Загрузка: %s\n", pageNum, *maxPages, url)
			items, err := parsePage(url)
			if err != nil {
				log.Printf(" [!] Ошибка на странице %s: %v\n", url, err)
				return
			}

			for _, item := range items {
				results <- item
			}
		}(pageURL, i)
	}

	wg.Wait()
	close(results)
	collectorWg.Wait()

	fmt.Println("------------------------------------------")
	fmt.Printf("[+] Парсинг завершен за %v. Собрано элементов: %d\n", time.Since(start), len(products))

	if err := exportToCSV(outputFile, products); err != nil {
		log.Fatalf("Ошибка экспорта в CSV: %v", err)
	}

	fmt.Printf("[+] Данные успешно сохранены в файл: %s\n", outputFile)
}

func parsePage(url string) ([]Product, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	var items []Product
	doc.Find(".quote").Each(func(i int, s *goquery.Selection) {
		title := strings.TrimSpace(s.Find(".text").Text())
		author := strings.TrimSpace(s.Find(".author").Text())
		link, _ := s.Find("a").Attr("href")

		if title != "" {
			items = append(items, Product{
				Title:  title,
				Author: author,
				Link:   "https://quotes.toscrape.com" + link,
			})
		}
	})

	return items, nil
}

func exportToCSV(filename string, data []Product) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	if err := writer.Write([]string{"Title", "Author", "Link"}); err != nil {
		return err
	}

	for _, item := range data {
		if err := writer.Write([]string{item.Title, item.Author, item.Link}); err != nil {
			return err
		}
	}

	return nil
}
