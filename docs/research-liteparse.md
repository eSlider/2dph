---
type: research
status: current
related:
  - PLAN.md
  - docs/runbook.md
---

# A/B: internal/ocr.PDFFile vs liteparse — качество md + PDF→JSON→YAML (issue #221)

Исследование в рамках эпика [#219](https://git.produktor.io/eSlider/2dph/issues/219)
(структурная экстракция документов). Сравнивали существующий PDF-конвейер 2dph
(baseline) с [liteparse](https://github.com/run-llama/liteparse) (candidate) на
трёх изолированных сэмплах из [#220](https://git.produktor.io/eSlider/2dph/issues/220):

- `invoice-text.pdf` — text-layer счёт: заголовок, таблица (3 строки), список.
- `invoice-scan.pdf` — scan-подобный: full-page изображение (тёмная полоса) + один текстовый run.
- `liteparse-sample.pdf` — публичный одностраничный PDF (lorem ipsum + заголовок).

Прод-корпус `var/corpus` не трогали; всё — в `var/research/` (gitignored).

## Как воспроизвести

```bash
# тесты (офлайн; docker-тест skip при отсутствии docker/образа)
go test -tags=research_ab ./bin/research/... -count=1

# прогон на samples (запускает liteparse через docker)
./bin/research/ab.go
```

Инструмент: `bin/research/ab.go` (build tag `research_ab`). Для каждого документа
гоняет: `lit is-complex` verdict, baseline (`internal/ocr.PDFFile`), `lit parse
--format markdown --extract-blocks --no-ocr` (+ OCR-вариант, если verdict просит),
и `lit parse --format json --extract-blocks` с конвертацией в YAML. Результат —
`var/research/out/metrics.json` + артефакты `*.md`, `*.ocr.md`, `*.json`, `*.yaml`.

Команда запуска liteparse (docker-обёртка в инструменте делает то же самое):

```bash
P=/usr/local/lib/pdfium-rs/chromium_7897/pdfium-linux-x64/lib
docker run --rm -e PDFIUM_LIB_PATH=$P -e LD_LIBRARY_PATH=$P \
  -v "$PWD/var/research/samples:/s:ro" -v "$PWD/var/research/out:/o" \
  ghcr.io/run-llama/liteparse:latest lit parse /s/<f>.pdf \
  --format markdown --extract-blocks --no-ocr -o /o/<f>.md
```

Образ `ghcr.io/run-llama/liteparse:latest` (lit 2.14.0). Путь pdfium и образ —
константы инструмента, переопределяются флагами `--pdfium-lib-path`/`--image`
или env `LITEPARSE_PDFIUM_LIB_PATH`/`LITEPARSE_IMAGE`.

## Метрики (прогон 2026-08-29, docker 26.1.4)

| Документ | verdict (`is-complex`) | baseline ms / байт | lit md ms / байт | lit md OCR ms / байт | lit json ms / байт | blocks (kind) | bboxes |
|---|---|---|---|---|---|---|---|
| invoice-text.pdf | needs-ocr (sparse-text) | 7 / 154 | 644 / 117 | 2519 / 117 | 2285 / 4860 | heading=1, paragraph=3 | 14 |
| invoice-scan.pdf | needs-ocr (scanned) | 23 / 11 | 699 / 11 | 2748 / 11 | 2552 / 665 | paragraph=1 | 1 |
| liteparse-sample.pdf | text (OCR не нужен) | 12 / 2851 | 672 / 2852 | — | 669 / 46969 | heading=1, paragraph=2 | 147 |

Разбор цифр:

- **baseline** (pdftotext fast path) — 7–23 ms. `internal/ocr.PDFFile` до OCR не
  доходит ни на одном сэмпле: у `invoice-scan.pdf` текстовый run `SCAN-9F31A1`
  делает fast path успешным. Tesseract на хосте не установлен — ветка
  gs+tesseract в этом прогоне не срабатывала (это честное поведение конвейера).
- **liteparse md** — 644–699 ms, из них ~0.5–0.7 s — старт контейнера docker;
  собственно lit тратит 1–25 ms (строки `[liteparse] total`). Для маленьких
  документов docker overhead доминирует.
- **OCR-вариант** (2.5–2.7 s) сработал для обоих invoice: у `invoice-text.pdf`
  результат байт-в-байт тот же, что и без OCR (117 байт) — OCR был не нужен,
  его спровоцировал ложный verdict `sparse-text`. У `invoice-scan.pdf` — только
  `SCAN-9F31A1` (тёмная полоса без текста — корректно пусто).
- **liteparse json** — для text-документов ~0.7 s, для scan — 2.5 s (OCR внутри).

## Качество markdown

| Проверка | invoice-text | liteparse-sample | baseline (pdftotext -layout) |
|---|---|---|---|
| Заголовок | `# INVOICE-7C2F9E` (level 1) | `# This is a simple PDF file.` (level 1) | строка текста |
| Таблица | **не распознана** — размазана в 3 параграфа, колонки/порядок потеряны | нет таблиц | колонки сохранены (layout) |
| Список / строки | `Client: Acme GmbH Item Widget A Widget B Total Note: net 14 days.` — всё в один параграф | — | строки разделены |
| Порядок чтения | нарушен: `Qty Price` и `2 10.00 1 25.00 45.00` вынесены отдельными параграфами | норм; строка `Sample PDF` вынесена параграфом перед заголовком | сохранён |

Вывод по md: для text-layer документов с таблицами **liteparse markdown хуже
baseline** — таблица не детектируется (`kind: table` отсутствует в blocks) и
размазывается в параграфы. Заголовки (уровень) liteparse определяет верно.

## PDF→JSON→YAML

`lit parse --format json --extract-blocks` даёт типизированную структуру:

```yaml
pages:
  - page: 1
    width: 612
    height: 792
    text: |-
      INVOICE-7C2F9E
      Client: Acme GmbH
      Item    Qty  Price
      Widget A  2       10.00
      ...
    text_items:            # 14 шт: геометрия + шрифт + confidence
      - text: INVOICE-7C2F9E
        x: 72
        y: 56.880005
        width: 129.808
        font_name: Helvetica
        font_size: 16
        confidence: 1
    blocks:                # семантика
      - kind: heading
        text: INVOICE-7C2F9E
        level: 1
        bbox: {x: 72, y: 56.88, width: 130, height: 19}
      - kind: paragraph
        text: "Client: Acme GmbH Item Widget A Widget B Total Note: net 14 days."
```

Конвертер: `bin/research/ab.go` → `JSONToYAML` (типизированные `LitJSON/LitPage/
LitItem/LitBlock`, `gopkg.in/yaml.v3`; yaml-теги явные, иначе yaml.v3 режет
подчёркивания: `total_pages` → `totalpages`).

Что даёт структура программно:

- **`pages[].text`** — плоский текст в порядке чтения, колонки по позиции
  сохранены (в отличие от md).
- **`text_items[]`** — 14 позиционированных фрагментов с bbox и confidence.
  Этого достаточно, чтобы восстановить таблицу: группировка по `y` → строки,
  сортировка по `x` → ячейки (например, `Total` и `45.00` лежат в одном ряду).
- **`blocks[]`** — только `heading`/`paragraph`; таблица как `kind: table` не
  распознаётся ни на одном сэмпле.
- **Извлечение полей счёта**: `INVOICE-7C2F9E` → первый block (`kind: heading`,
  `level: 1`); `Client: Acme GmbH` → вторая строка `page.text` / `text_items[1]`;
  сумма `45.00` → по y-бакетам `text_items`. Извлекается программно, но через
  геометрию `text_items`, а не через семантику `blocks`.

## Вывод

- **Заменять `internal/ocr.PDFFile` / `pdfHandler` на liteparse не стоит.**
  Для text-layer документов baseline быстрее на 1–2 порядка (7–23 ms против
  640+ ms) и лучше сохраняет таблицы в тексте.
- **Ценность liteparse — не md, а структурированный JSON** (bbox, шрифт,
  confidence, порядок чтения). Это то, чего у pdftotext нет, и что полезно для
  brain ingest (контент-адресация `#100` — координаты узлов) и OO Documents.
- **Рекомендация — гибрид**: fast path pdftotext (текстовые PDF, <50 ms) +
  liteparse JSON только для сложных/скан-документов. Порог выбора по
  `lit is-complex` использовать с осторожностью: verdict `sparse-text` на
  маленьком text-layer счёте — ложное срабатывание (лишние ~2.5 s OCR на
  документ, где OCR не нужен).
- **Docker overhead** (~0.5–0.7 s на `docker run`) убивает выигрыш на мелких
  документах; в проде liteparse держать как демон/долгоживущий контейнер,
  а не запускать контейнер на каждый документ.
- **Таблицы из text-layer PDF** ни один метод не распознаёт как структуру:
  pdftotext сохраняет колонки текстом, liteparse размазывает в параграфы.
  Настоящее распознавание таблиц — post-processing по `text_items` (y/x-бакеты)
  либо OCR-путь для сканов.

Итог: **не замена, а дополнение** — гибрид «pdftotext fast path + liteparse
JSON для сложных + геометрия `text_items` для структурного извлечения».
Следующий шаг эпика #219 — PoC гибридного handler'а с `text_items`-реконструкцией
таблиц на реальном корпусе.

---

## Оптимизация: start-as-service + struct-data ETL (2026-08-30)

### Паттерн «демон-контейнер» подтверждён замерами

`docker run` на каждый документ платит 0.5–0.7s за lifecycle контейнера
(overlay mount + bridge network + `--rm`). Решение — **start-as-service**:
один долгоживущий контейнер, вызов через `docker compose exec`.

```bash
docker compose up -d liteparse                 # warmup при старте (entrypoint)
docker compose exec -T liteparse lit parse /v/<rel>.pdf --format json -o /data/work/<name>.json
```

Warmup в `scripts/liteparse-entrypoint.sh` гоняет `lit --version` + один parse
при старте — pdfium/tesseract попадают в page cache, первый холодный OCR не
платит 2.5s.

**p95 (n=10, прогон 2026-08-30, docker 26.1.4):**

| Паттерн | text md p95 | text json p95 | scan OCR p95 |
|---|---|---|---|
| `docker run` на документ (старый A/B) | ~700 ms | ~700 ms | ~2.7 s |
| **демон-контейнер `docker compose exec`** | **118 ms** | **113 ms** | **312 ms** |

Вывод: демон-паттерн ускоряет text-путь ~**6x**, OCR ~**8.7x**. OCR 312ms —
это реальная работа tesseract (не overhead); 2.5s холодного OCR при первом
запуске убирается warmup'ом.

### struct-data ETL: документ → YAML по hash

`bin/research/convert.go` — любой документ (pdf/docx/xlsx/pptx/odf/image) →
литерparse JSON (тёплый сервис) → YAML в `var/struct-data/<sha256>.yml`,
**content-addressed** (hash исходника). Идемпотентно: существующий непустой
YAML → skip, повторный запуск не пере-оцифровывает.

```bash
./bin/research/convert.go <file-or-dir>...    # → var/struct-data/<hash>.yml
./bin/research/convert.go --ocr <scan.pdf>    # OCR-путь
./bin/research/convert.go --list              # список var/struct-data
```

Обёртка YAML (meta + document):

```yaml
meta:
  hash: <sha256 исходника>
  source_path: var/research/samples/invoice-text.pdf
  extension: pdf
  format: pdf
  size: 922
  created_at: ...      # mtime файла
  modified_at: ...
  digitized_at: ...    # время оцифровки
  engine: liteparse
  ocr: false
  blocks: true
  original_name: invoice-text.pdf
document:              # литерparse структурированный JSON→YAML
  total_pages: 1
  pages: [page, width, height, text, text_items[], blocks[]]
```

Это и есть то, что нужно для CRM/факт-экстракции: metadata (даты, путь/URL,
extension, формат, время оцифровки) + структура (`text_items` bbox, `blocks`).
Следующий этап (вне данного исследования) — YAML association mapping в CRM /
fact-audit: чтение `var/struct-data/*.yml` как источника фактов.

Типы liteparse + демон-exec раннер вынесены в `internal/research` (single
implementation): используются и `bin/research/ab.go`, и `convert.go`, и
`bench.go`. Инструмент замеров: `bin/research/bench.go` (warmup + p50/p95).
