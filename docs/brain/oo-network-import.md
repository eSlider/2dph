# OO CRM: коннектор accept-связей сети (N-1.2 #269)

Перенос accept-связей сети связей (L-9.5 #234, выход
`bin/network --accept-only`) в OnlyOffice CRM как **деловых контактов** —
чтобы маркетинг/продажи работали из графа: каждый контакт с premises-
ссылкой (проверяемой в графе через `bin/network`) и без дублей
(ADR-0012 §сеть связей п.4). Пилот — top-60 accept
andriy.oblivantsev@wheregroup.com (N-1.3 #270).

## Инструмент

```bash
bin/network/network.go --person X --accept-only > /tmp/net.yml   # манифест (read-only)
ONLYOFFICE_URL/USER/PASS ./bin/onlyoffice/import-network.go --manifest /tmp/net.yml   # report only
./bin/onlyoffice/import-network.go --manifest /tmp/net.yml --dry-run                   # preview: lookups, ничего не пишет
./bin/onlyoffice/import-network.go --manifest /tmp/net.yml --write --limit 60          # создать до 60
cat /tmp/net.yml | ./bin/onlyoffice/import-network.go --manifest - --write             # stdin
```

Флаги: `--manifest <file|->` (обязателен), `--dry-run` (preview, ничего не
пишет), `--write` (создавать; по умолчанию report-only), `--limit N`
(макс. новых за прогон), `--tag` (default `2dph:network:<target-email>`).
Отчёт: `created / matched / skipped / failed / pending`; сбои — списком
(`failed` возвращает exit 1). Creds — `ONLYOFFICE_URL/USER/PASS`
(go-onlyoffice `GetEnvironmentCredentials`), не логируются.

## Маппинг (связь → контакт)

| Что в манифесте | Что в OO CRM |
|---|---|
| accept-связь (kind person/company) | контакт **Person**: `firstName/lastName` из `name` (fallback — локальная часть email, `mailconv.SplitPersonName`); `about` — сводка |
| email связи | `AddContactInfo` infoType email, category Work, **primary** (ключ идемпотентности, lowercase) |
| источник | тег контакта `2dph:network:<target-email>` (создаётся, если нет) |
| сила связи | `about`: `{msgs} писем / {threads} тредов / {replies} ответов, период {period}` |
| premises (первые 5 + extraPremises) | `about`: `premises: mail <Message.id>; commit <repo:sha>; … (+N ещё)` — стабильный ключ в kb.lbug |
| источник манифеста | `about`: `source: <manifest.Source>` (текст из манифеста) |

«Деловая связь» в OO = существование контакта с email + тегом источника
(нативной записи Person↔Person в OO нет — не выдумываем). Полный манифест
(все premises, projects) — артефакт вне CRM (`--json` у bin/network).

## Границы классов (по N-1.1 #268)

- **kind=service** (GitLab/PayPal/LinkedIn-служебные/markets-platform/
  трекеры/рассылки) — пропускаются (`skipped`), в CRM не идут;
- **linkedin-релеи людей** (`hit-reply@linkedin.com` с реальным display
  name: Ruby Rodriguez и т.п.) — Person как есть: email = релей платформы,
  имя = реальный человек (классификатор N-1.1: person);
- **роль-ящики/компании** (`info@`/`alle@`, организации) — тоже контакт
  Person (fallback-имя из локальной части): компания-группировка по домену
  и дедуп существующих — отдельный шаг N-1.4 #271.

## Идемпотентность

Ключ = email (lowercase). Один проход по CRM (`BuildContactEmailIndex`);
существующий по email (включая импортированных из VCF/MAB #85) — **skip**
(`matched`), не перезаписывается (ручные правки не трогаем). Повторный
прогон = 0 созданий / 0 изменений. При сбое email/тег-прикрепления только
что созданный контакт удаляется (rollback) — «created» = полностью
созданный контакт, повтор не заводит дубль.

## Код и тесты

- `internal/network/manifest.go` — контракт манифеста `network.Manifest`
  (общий у продюсера `bin/network` и коннектора; раньше — crmDoc/crmLink в
  bin/network);
- `internal/ooimport` (cgo-free): `ParseManifest` → `BuildPlan` (маппинг,
  фильтр сервисов, тег, about) → `Run` (lookups + создание, dry-run/limit);
- CLI `bin/onlyoffice/import-network.go` (shebang, tag
  `onlyoffice_import_network`);
- тесты: офлайн (`internal/ooimport/*_test.go` — план/маппинг/roundtrip
  продюсер-консьюмер, split по existing: повтор = 0 новых) + интеграция
  (`//go:build integration`, живой OO, creds из окружения, без creds —
  skip; созданный тестовый контакт удаляется в конце; тег остаётся
  0-count — API клиента не удаляет теги).

## Ссылки

- Epic N-1 #267; дети: N-1.1 #268 (фильтр), N-1.3 #270 (пилот), N-1.4 #271
  (компании).
- Сеть: docs/brain/graph-network.md; фильтр: docs/brain/crm-network-filter.md.
- go-onlyoffice: примитивы FindPersonByEmail/BuildContactEmailIndex/
  CreatePerson/AddContactInfo/CreateContactTag/AddContactTag.
