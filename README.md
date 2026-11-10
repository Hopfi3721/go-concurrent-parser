# Concurrent Web Scraper (Go CLI)

[![ci](https://github.com/Hopfi3721/go-concurrent-parser/actions/workflows/ci.yml/badge.svg)](https://github.com/Hopfi3721/go-concurrent-parser/actions/workflows/ci.yml)
[![license](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Консольный парсер на Go: обходит страницы в несколько горутин, уважает лимиты
целевого сайта и выгружает результат в CSV, который нормально открывается в Excel.

Проверено на [quotes.toscrape.com](https://quotes.toscrape.com) (сайт-песочница для обучения парсингу).

## Возможности

- **Параллельный обход** — горутины + семафор ограничивают число одновременных запросов
- **Rate limit** — `-delay` задаёт минимальный интервал между запросами ко всему сайту, а не к одной горутине
- **Retry с backoff** — повторы на сетевых ошибках, `429` и `5xx`; `Retry-After` учитывается
- **Отмена по Ctrl+C** — контекст останавливает работу, уже собранные данные сохраняются
- **Настраиваемые селекторы** — `-item/-title/-author/-link` позволяют разобрать другой сайт без правки кода
- **CSV для Excel** — UTF-8 с BOM и разделителем `;`, переводы строк внутри ячеек схлопываются

## Установка

```bash
go mod download
go build -o parser .
```

## Использование

```bash
# 5 страниц сайта по умолчанию -> output.csv
./parser

# 10 страниц, 10 потоков, без задержки
./parser -pages=10 -concurrency=10 -delay=0 -out=quotes_fast

# Свой сайт: пагинация через ?page=N и свои селекторы
./parser -url=https://example.com/catalog -page-pattern="?page=%d" \
         -item=".product-card" -title=".product-card__name" \
         -author=".product-card__price" -link="a.product-card__link"
```

### Флаги

| Флаг | По умолчанию | Описание |
| :--- | :--- | :--- |
| `-url` | `https://quotes.toscrape.com` | Базовый URL сайта |
| `-page-pattern` | `/page/%d/` | Шаблон пути страницы, `%d` — номер |
| `-pages` | `5` | Сколько страниц обойти |
| `-out` | `output` | Имя CSV (расширение добавится само) |
| `-concurrency` | `5` | Максимум одновременных запросов |
| `-delay` | `500` | Минимальный интервал между запросами, мс (`0` — выключить) |
| `-retries` | `3` | Повторы на сетевой ошибке, `429` и `5xx` |
| `-timeout` | `10s` | Таймаут одного запроса |
| `-item` | `.quote` | CSS-селектор карточки |
| `-title` | `.text` | CSS-селектор заголовка внутри карточки |
| `-author` | `.author` | CSS-селектор автора внутри карточки |
| `-link` | `a[href^='/author/']` | CSS-селектор ссылки внутри карточки |

## Формат выгрузки

`output.csv`, UTF-8 с BOM, разделитель `;`:

| Заголовок | Автор | Ссылка |
| :--- | :--- | :--- |
| The world as we have created it... | Albert Einstein | https://quotes.toscrape.com/author/Albert-Einstein |
| It is our choices, Harry... | J.K. Rowling | https://quotes.toscrape.com/author/J-K-Rowling |

## Как адаптировать под другой сайт

1. Открыть одну страницу листинга в браузере и посмотреть структуру карточки.
2. Подставить CSS-селекторы во флаги `-item/-title/-author/-link`.
3. Проверить пагинацию: если страницы открываются как `?page=2`, ставить `-page-pattern="?page=%d"`.
4. Начать с `-concurrency=1 -delay=1000`, убедиться, что данные корректны, и только потом ускорять.

## Тесты

```bash
go test ./...
```

Покрыто: разбор HTML на фикстуре, разрешение относительных ссылок, retry-логика
(повтор на `5xx`, отсутствие повтора на `404`), отмена контекста, сборка URL,
CSV с BOM и экранированием, валидация флагов.

## Ограничения

- парсится только статический HTML: SPA и контент, который рисует JavaScript, не поддерживаются
- пагинация задаётся вручную, автоопределение «следующей страницы» не реализовано
- `robots.txt` не проверяется — перед использованием на чужом сайте проверьте правила и условия сервиса

## Лицензия

MIT
