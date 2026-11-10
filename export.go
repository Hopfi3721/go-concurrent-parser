package main

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
)

func ensureCSVExtension(filename string) string {
	ext := filepath.Ext(filename)
	if ext == "" {
		return filename + ".csv"
	}
	if !strings.EqualFold(ext, ".csv") {
		return strings.TrimSuffix(filename, ext) + ".csv"
	}
	return filename
}

func exportToCSV(filename string, data []Product) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// BOM нужен, чтобы Excel открыл UTF-8 без кракозябр.
	if _, err := file.WriteString("\xEF\xBB\xBF"); err != nil {
		return err
	}

	writer := csv.NewWriter(file)
	// Точка с запятой — разделитель, который ждёт Excel с русской локалью.
	writer.Comma = ';'
	defer writer.Flush()

	if err := writer.Write([]string{"Заголовок", "Автор", "Ссылка"}); err != nil {
		return err
	}

	for _, item := range data {
		row := []string{
			cleanText(item.Title),
			cleanText(item.Author),
			strings.TrimSpace(item.Link),
		}
		if err := writer.Write(row); err != nil {
			return err
		}
	}

	writer.Flush()
	return writer.Error()
}

// cleanText схлопывает переводы строк и повторные пробелы:
// CSV-ячейка должна быть одной строкой.
func cleanText(text string) string {
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\r", " ")
	text = strings.ReplaceAll(text, "\t", " ")

	return strings.Join(strings.Fields(text), " ")
}
