package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureCSVExtension(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "output", want: "output.csv"},
		{input: "output.csv", want: "output.csv"},
		{input: "output.CSV", want: "output.CSV"},
		{input: "output.txt", want: "output.csv"},
		{input: "data/quotes.json", want: "data/quotes.csv"},
	}

	for _, tt := range tests {
		if got := ensureCSVExtension(tt.input); got != tt.want {
			t.Errorf("ensureCSVExtension(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCleanText(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "  hello   world  ", want: "hello world"},
		{input: "line1\nline2", want: "line1 line2"},
		{input: "tab\there", want: "tab here"},
		{input: "", want: ""},
	}

	for _, tt := range tests {
		if got := cleanText(tt.input); got != tt.want {
			t.Errorf("cleanText(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestExportToCSV(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.csv")
	data := []Product{
		{Title: "The world as we have created it", Author: "Albert Einstein", Link: "https://quotes.toscrape.com/author/Albert-Einstein"},
		{Title: "Line one\nline two", Author: "Some; Author", Link: "https://example.com"},
	}

	if err := exportToCSV(path, data); err != nil {
		t.Fatalf("exportToCSV: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	content := string(raw)
	if !strings.HasPrefix(content, "\xEF\xBB\xBF") {
		t.Error("file does not start with UTF-8 BOM")
	}
	if !strings.Contains(content, "Заголовок;Автор;Ссылка") {
		t.Errorf("missing header with semicolon delimiter, got:\n%s", content)
	}
	if !strings.Contains(content, "Line one line two") {
		t.Error("newlines inside a cell were not cleaned")
	}
	if !strings.Contains(content, `"Some; Author"`) {
		t.Error("semicolon in a cell must be quoted")
	}
}

func TestExportToCSVEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.csv")

	if err := exportToCSV(path, nil); err != nil {
		t.Fatalf("exportToCSV: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if lines := strings.Count(strings.TrimSpace(string(raw)), "\n"); lines != 0 {
		t.Errorf("empty export has %d extra lines, want only header", lines)
	}
}
