# ADR P-9.6: память 2dph — единая модель фактов, аудита и хранения

> Статус: **Accepted** (2026-09-03). Документ не вводит новых механизмов: он
> сводит уже принятые и работающие решения (P-9.2/#235, P-9.3/#13,
> P-9.4/#240, L-9/#229–234) в одну модель для клиентов. Изменение модели —
> через новый issue; эпик P-9 (#239).

## 1. Цель и место документа

2dph («память», префронтальная кора по ADR-0012) держит **факты и выводы**:
leafs (утверждения) + связи + аудит. Разрозненные документы описывали части
модели; этот ADR — единая точка входа для клиентов (агенты/MCP, gator, cv,
скрипты), чтобы все опирались на одну модель фактов/аудита/хранения.

Что он делает и чего не делает:

- **сводит** уже существующее: write-контракт (`docs/brain/contract.md`,
  P-9.2/P-9.3), read-контракт (`docs/brain/read-contract.md`, P-9.4),
  эмпирику аудита (`docs/facts/audit-recipes.md`, #52/#56) и формальную
  логику L-9 (issues #229–234), факт-слой (`bin/facts/extract.go`,
  `docs/facts/ingest.md`);
- **не дублирует код и не заменяет** контракты: здесь — модель и ссылки,
  норматив — в перечисленных файлах (пересечение с L-9 файловое минимальное:
  ADR отдельный файл, L-9 пишет в `docs/facts/audit-recipes.md` и
  `var/audit/source-of-truth.yml`);
- **не описывает** инструменты сбора/CRM/gator — их зоны фиксирует ADR-0012
  (ответственность зон, «один канон на тип данных»).

Карта зон (ADR-0012), где живёт 2dph:

| Зона | Система | Канон |
|------|---------|-------|
| сенсоры/сырьё | gator (`var/` raw/parquet) | **данные** (raw, версии, append-only) |
| кора/память | **2dph** (Ladybug `var/kb.lbug`) | **факты/выводы** (leafs) + связи |
| скелет | OnlyOffice CRM | структура бизнеса (сделки/контакты/задачи) |
| суперэго | skills/inventar | правила, контракты, ADR |

2dph ссылается на каноны данных и CRM, не копирует их; свой канон (факты/
выводы) держит сам. Детали — ADR-0012 (внешний документ fabric, inventar
`docs/adr/ADR-0012-responsibility-zones.md`).

## 2. Модель фактов: термины

Единые термины (совпадают с write- и read-контрактами):

| Термин | Определение |
|--------|-------------|
| **наблюдение** (leaf корпуса) | единичное свидетельство одного источника: корпусный лист (`root=info`, kind `mail`/`commit`/`chat`/`reference`/`seed`/…). Пишется адаптером корпуса из одного файла/коммита/сообщения — по построению **single-source**, само по себе ничего не утверждает. Момент «когда контент увиден» фиксирует `observed_at` (RFC3339). |
| **info** | корень нарративных/описательных листов корпуса (mail/git/chats/docs). Поисковый слой, `never asserted`. Один лист = одно наблюдение. |
| **факт** | утверждение под `root=facts`, подтверждённое **≥2 независимыми источниками**; `confidence: confirmed`. Единственный корень, ответ с которого можно выдавать как подтверждённое знание. |
| **независимость источников** | источники из разных классов свидетелей: runtime × declared (например `docker ps` × compose-файл; `~/.ssh/config` × README/PLAN/AGENTS). В листе пара пишется в `source` через `" x "` (нотация факт-слоя, `facts/extract`). |
| **версия** | одна версия = один id. id записи = `ContentHash(source\|external_id\|kind\|text)[:32]` (dedup-ключ == id). Изменение контента/identity → новый hash → новая версия; старая остаётся в графе. Повторная запись той же версии — MERGE (upsert). |
| **валидность** | `valid_from` / `valid_to` (`YYYY-MM-DD`, D24) — интервал истинности **факта по дням**, ортогонален версии (версия про запись, валидность про смысл). Пустой интервал = «всегда» (legacy); пустой `valid_to` = открытый конец; концы включительно. Фильтр — `--as-of YYYY-MM-DD`. |
| **confirmed** | `confidence: confirmed`; пустое значение confidence (legacy leafs) трактуется как `confirmed`. |
| **(not confirmed)** | любой ответ, который **не сошёл с facts-корня**: info-хит, hypothesis/partial, единственный источник, противоречие 2v2 без сработавшего правила. Агент обязан помечать такой ответ `(not confirmed)`, а не выдавать за факт. |
| **stale** | `valid_to` в прошлом, либо факт отсутствует в сегодняшнем `--as-of`-виде (D24). Не путать с D16 `temporal_freshness` (свежесть источника против HEAD). |

Один лист в БД — контрактные поля против runtime-схемы Ladybug:

| Контракт (`internal/contract.Leaf`) | Колонка/смысл в БД |
|-------------------------------------|--------------------|
| `source` | имя корпуса-источника: `mail`/`git`/`chats`/`docs`; у фактов — пара источников `"S1 x S2"` |
| `external_id` | устойчивый ref внутри корпуса (message id / commit sha / chat id / rel-путь; у mail — content-address `sha256(text)[:16]`) |
| `kind` | тип листа (в БД колонка `type`): `fact`/`mail`/`chat`/`commit`/`reference`/`seed`/… |
| `text` | содержимое (Heading + body, единообразно во всех корпусах) |
| `root` | `facts` (утверждение, ≥2 источника) или `info` (нарратив); default `info` |
| `confidence` | открытый домен: `confirmed`/`estimated`/`inferred`/`hypothesis`/`partial`/… |
| `valid_from`/`valid_to` | D24-день валидности (см. выше) |
| `observed_at` | когда контент увиден (RFC3339); **вне** dedup-ключа |

Норматив записи — [`docs/brain/contract.md`](contract.md) (Leaf.Validate(),
`NormalizeText`, ContentHash, `observed_at` вне хэша, адаптеры корпуса).

## 3. Модель аудита

### 3.1 Как факт подтверждается

1. **Наблюдения** приходят из корпусов (адаптеры `internal/corpus`) и пишутся
   всегда как `root=info` (`internal/brain/corpus.go`, `WriteCorpus`) — один
   лист происходит из одного файла/коммита, сам по себе фактом быть не может.
2. **Факт-слой** — отдельный evidence gate (`bin/facts/extract.go`), не корпус:
   сопоставляет два независимых класса источников —
   S1 runtime (`docker ps` / `~/.ssh/config`) × S2 declared
   (compose-файл / README+PLAN+AGENTS) — и пишет совпадение как
   `root=facts`, `confidence: confirmed`, `source: "S1 x S2"`.
3. **Promote** (`bin/facts/promote.go`): уже подтверждённые `info`-листы,
   чей `source` содержит ≥2 различных источника, перекореняются в `facts`
   (идемпотентно).
4. При **rebuild** факт-слой включается только флагом `--with-facts`
   (`bin/brain/index.go`); без него facts-корень пуст (#181) — см.
   `docs/facts/ingest.md`.

Проверка двух-источниковости по живой БД — `bin/facts/audit.go db` (каждый
`root=facts` лист); корень правила «≥2 источника» — evidence-first (AGENTS.md),
эмпирика — `docs/facts/audit-recipes.md` (#52/#56).

### 3.2 Когда ответ (not confirmed)

- ответ сошёл с `info`-корня (нарратив, даже если `confidence: confirmed`);
- у утверждения один источник (hypothesis/partial);
- **противоречие 2v2**: `a x b` против `c x d`, обе стороны с ≥2 источниками,
  но ни одно правило не сработало (`temporal_freshness` / `authority_pairing`,
  D16) — остаётся hypothesis, пока не расследовано (`bin/facts/audit.go
  contradict`);
- stale (valid_to в прошлом или нет в сегодняшнем `--as-of`).

Дедукционный порядок поиска: **facts → info → web** (`web` — SearXNG, второй
независимый источник, держится отдельным блоком; `throttled`/`refused` — не
доказательство отсутствия).

### 3.3 Формальная логика L-9 (Vinogradov)

Эмпирика рецептов (#52/#56) дополнена формальными законами на уровне
суждений/URL (L-9, issues #229–234; ядро `internal/facts`, cgo-free):

| Закон | Правило | Вердикт |
|-------|---------|---------|
| identity (тождество) | один канонический URL в разных написаниях → merge/flag | `weaken` |
| contradiction (противоречие) | подтверждённые `P` и `¬P` об одном предикате одного URL с пересекающимися D24-интервалами | `reject` |
| excluded middle | hypothesis/partial или claim без предиката — не FACT, кандидат в `open_questions[]` | `weaken` |
| sufficient reason | verdict `accept` только с FACT-/OPEN- premises | `reject`/`weaken` |

Суждение = факт о ресурсе URL: `{id, url, claim, attr, neg, conf, from, to}`
(`attr` — предикат, `neg` — знак P/¬P). Машинные проверки —
`bin/facts/audit.go formal` (stdin JSON). Канонизация URL —
`facts.CanonicalURL` (конвенция gator G-8.1). Полные рецепты —
`docs/facts/audit-recipes.md` (§Формальные проверки URL, L-9.3).

### 3.4 Аудит-карточки и source-of-truth

Каждый вердикт фиксируется **карточкой Vinogradov** в логическом SoT
(`var/audit/source-of-truth.yml`, `audits[]`, L-9.1 #230 / L-9.2 #231):
рекомендация без карточки = нарушение. Карточка обязана опираться на
FACT-/OPEN- premises:

```bash
bin/facts/audit-card.go --claim "…" --premises FACT-9002,FACT-9001 \
  --inference deduction --gaps OPEN-0001 --counter none --verdict weaken
```

- CLI валидирует карточку (verdict `accept|reject|weaken`, inference
  `deduction|induction|analogy|other`, обязательные claim/premises/counter),
  присваивает `AUD-NNNN`, дописывает в `audits[]` (yaml-дерево, остальные
  секции не трогает).
- SoT хранит только индексированные факты `{id, claim, source}` — срезы из
  `root=facts` Ladybug; сырьё остаётся в `var/`; секретов нет (ASR-0006);
  файл gitignored, живёт локально как артефакт контура.
- В `root=facts` **нет** фактов-вакансий (truth-gate #52) — см. OPEN-0001 в
  SoT: не выдуманы.

### 3.5 Read-аудит (форматы ответа)

- `/audit` (HTTP/MCP) и `bin/facts/audit.go db` — гистограмма `root ×
  confidence` (формат — read-контракт, поле `by_confidence`);
- `bin/brain/audit-contract.go` — read-only аудит **соответствия write-
  контракту** (пропуски обязательных полей, распределение по корпусам);
- `bin/brain/read-contract.go` / `read-contract-db.go` — гейты форматов
  read-контракта (живой сервис и живая БД, read-only).

## 4. Модель хранения

### 4.1 Ladybug, один файл, два корня

Вся память — один встроенный граф Ladybug: `var/kb.lbug`. `facts` и `info` —
колонка `root` на каждом листе, **семантическое, не физическое** разделение:
оба корня живут в одном файле и пишутся в одной транзакции (D12, ACID — аудит
видит согласованный снимок).

Схема (`internal/brain` `InitSchema`, идемпотентна): узлы
`Leaf`/`File`/`Host`/`Commit`/`Person`/`Message` + рёбра
`FROM_FILE`, `RUNS_ON`, `AUTHORED`, `SENT`, `TO`, `CC`, `BCC`, `REPLY_TO`,
`SYNAPTIC` (пользовательские связи leaf↔leaf, #82). Лист несёт embedding
`FLOAT[256]`; ANN-индекс — `var/state/vector.ann` (dim=256, nlist=2000,
nprobe=2000, #206). Схема Ladybug — внутренняя, меняется без предупреждения;
**клиенты никогда не парсят `var/kb.lbug` напрямую**, только read-контракт.

### 4.2 Запись: контракт + адаптеры корпуса + факт-слой

| Путь записи | Инструмент | Что пишет |
|-------------|------------|-----------|
| корпуса (docs/mail/chats/git) | адаптеры `internal/corpus` (`contract.Source`: `Name()` + `Stream(ctx, emit(Leaf))`) → драйвер `bin/brain/index.go` → `brain.WriteCorpus` | `root=info`, по одному наблюдению на файл/commit/сообщение |
| факты | `bin/facts/extract.go` → `UpsertLeaf` (напрямую, без `FROM_FILE`) | `root=facts`, `kind=fact`, пара источников в `source` |
| ручное добавление | `bin/brain/add.go` (`--root facts --source "a.md x b.md"`) | лист по контракту |
| rebuild | `bin/brain/index.go --rebuild --with-mail --with-facts [--with-chats] [--git-root DIR]` | полный рехэш id = ContentHash, детерминированно |

Факты пишутся **без `external_id`** (21 лист на живой БД) — прямой путь
`UpsertLeaf` зафиксирован контрактом как допустимый (P-9.3, расхождение
описано в `docs/brain/contract.md`). `observed_at` штампуется при записи,
если пуст.

### 4.3 Чтение: read-контракт (один формат на всех поверхностях)

Read-контракт (`internal/contract/read.go`, версия **`1.0`**, MAJOR.MINOR):
каждый ответ несёт `contract_version`; additive-правки — MINOR, breaking —
MAJOR. Клиенты обязаны принимать любой ответ с тем же MAJOR, игнорировать
неизвестные поля, отсутствие `contract_version` (сервис до контракта)
пропускать через `--relax-version`.

| Поверхность | Команда / путь | Формат |
|-------------|----------------|--------|
| CLI | `bin/brain/search.go "q" --json`, `get.go <id> --body --json`, `stats.go --json` | read-контракт |
| HTTP (:8630) | `GET /search?q=`, `GET /get?id=`, `GET /stats`, `GET /audit`, `GET /openapi.json` | read-контракт (+ raw-граф: `/leafs`, `/edges`, `/path`) |
| MCP | `POST /mcp`: tools `search` / `get` / `audit` (минимальная поверхность, D20; `stats`/`ingest` — HTTP-only) | read-контракт |
| гейты | `bin/brain/read-contract.go`, `read-contract-db.go` (read-only) | валидация схемы |

Норматив форматов (search/get/stats/audit, отсутствие → отсутствие,
`results` всегда массив) — [`docs/brain/read-contract.md`](read-contract.md).

### 4.4 Актуальное состояние БД (после полного rebuild, 2026-09-03)

Замеры на живой БД (`/stats`, `/audit` сервиса :8630 и read-only
`bin/brain/audit-contract.go`):

| Метрика | Значение |
|---------|----------|
| total leafs | **105,220** |
| info | **105,199** = mail 105,027 + docs 172 |
| facts | **21** = 19 docker/compose + 2 ssh-host (все `confidence: confirmed`, двух-источниковые) |
| root × confidence | facts 21 confirmed; info 105,199 confirmed |
| ANN | 105,220 (loaded, `var/state/vector.ann`, dim=256) |
| соответствие контракту | observed_at/how/loc/kind: 0 пропусков; `external_id` отсутствует ровно у 21 facts-листа (легальный путь записи фактов) |

Контекст цифр: полный rebuild 2026-09-02 (P-9.3 content-address dedup) сжал
307,431 стримнутых mail-листов → 105,027 уникальных (live `var/corpus/mail` +
legacy `var/mail` схлопнулись по ContentHash, #248). Ранее
задокументированные ориентиры **сняты**: «info ≥ ~200k» и «~313k leafs»
(см. §7). Между гейтом read-контракта 2026-09-02 (105,210) и замером
2026-09-03 БД выросла на 10 docs-листов; нормативны актуальные цифры выше.

## 5. Границы: что brain НЕ делает

- **Inspector/auditor, не executor** (#52): 2dph не пишет состояние
  job-пайплайна/cv/CRM, не отправляет сообщения, не меняет источники. Он
  фильтрует, ищет, аудирует; единственная его запись — собственный канон
  (leafs/факты/выводы) в `var/kb.lbug` (+ граф-слои Commit/Person/Message).
- **Канон один на тип данных** (ADR-0012): данные → gator, структура бизнеса
  → OnlyOffice CRM, правила → inventar/skills. 2dph **ссылается**, не
  копирует; импорт в граф (mail/git) — premises/связи, не дубль канона.
- **Дедукция не выходит за посылки** (границы рецептов L-9.4/L-9.5):
  «что сделал» по message/files — гипотеза, не факт о намерении; 1 коммит ≠
  «вся неделя на X»; алиасы email не склеиваются (сопряжение строго по
  email); транзитивные цепочки «знакомые знакомых» — вне пилота (depth=1);
  mail↔repo привязка не выдумывается.
- **facts без выдумок**: в `root=facts` только двух-источниковые утверждения;
  фактов-вакансий нет (SoT OPEN-0001), пока их не подтвердят ≥2 источника.
- **Ответственность за подтверждение**: агент не выдаёт info/одиночный
  источник за факт — обязателен маркер `(not confirmed)`; `web`-блок не
  смешивается с графовыми hits.

## 6. Карта потребителей

| Потребитель | Поверхность | Контракт / инструмент |
|-------------|-------------|----------------------|
| агенты (PicoClaw gateway, `cv/ai-bot`) | MCP `search → get → audit` (`POST /mcp`, :8630) | read-контракт (P-9.4); скилл `picoclaw`, `brain` |
| gator и будущие сервисные клиенты | HTTP/CLI `--json` | read-контракт; P-9.5 (#241) клиенты поверх него; запись gator — свой контракт `contract.Record` (G-8.0, #73) |
| cv / job-пайплайн | `search → get → audit`, `/stats` | read-контракт + аудит-рецепты (#52) |
| скрипты стека | CLI `bin/brain/*.go --json` | read-контракт (версия `1.0`); гейты — `read-contract*.go` |
| оператор/аудитор | `bin/brain/*`, `bin/facts/*`, `audit-card.go` | write-контракт (запись), read-контракт, SoT, L-9 |

Все клиенты обязаны: принимать MAJOR-совместимые ответы, игнорировать
неизвестные поля, помечать не-facts ответы `(not confirmed)` и не парсить
`var/kb.lbug` напрямую.

## 7. Что изменено против ранее задокументированного

- **Снято правило «info ≥ ~200k»** (было в `skills/brain/SKILL.md`): после
  полного rebuild 2026-09-02 актуальный ориентир — **total 105,220** =
  info 105,199 + facts 21 (см. §4.4). Меньше → корпус недозаписан.
- **Снят ориентир «~313k leafs»**: P-9.3 content-address dedup схлопнул
  дубликаты mail (307k → 105k). Исторические 313k остаются только как
  контекст в runbook (замеры ANN-параметров #192/#206).
- Старые снапшоты в `docs/brain/contract.md` («на момент P-9.3», 313,817
  leafs, «ещё не пересобрана») описывают состояние **до** rebuild; актуальные
  цифры — §4.4 этого ADR.

## 8. Cross-ref

- Write-контракт: [`docs/brain/contract.md`](contract.md) (P-9.2/#235,
  P-9.3/#13) + `internal/contract` (Leaf, ContentHash, адаптеры).
- Read-контракт: [`docs/brain/read-contract.md`](read-contract.md)
  (P-9.4/#240) + `internal/contract/read.go` (`contract_version 1.0`).
- Аудит: [`docs/facts/audit-recipes.md`](../facts/audit-recipes.md) (#52/#56),
  L-9 (#229–234), SoT `var/audit/source-of-truth.yml` (L-9.1 #230),
  `bin/facts/audit-card.go` (#231), формальные проверки (#232).
- Факт-слой: `bin/facts/extract.go`, `docs/facts/ingest.md`, rebuild —
  `docs/brain/rebuild.md`.
- Дизайн/дедукция: `docs/design.md` (D12/D16/D20/D24), скилл `brain`
  (`skills/brain/SKILL.md`), `docs/picoclaw.md` (агентский профиль).
- Зоны/каноны: ADR-0012 (ответственность зон), gator G-8.0 (#73) —
  контракт записи данных; P-9.5 (#241) — сервисные клиенты.
