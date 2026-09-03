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
./bin/onlyoffice/import-network.go --manifest /tmp/net.yml --write --backfill          # + дописать тег/about matched (N-1.4)
./bin/onlyoffice/import-network.go --manifest /tmp/net.yml --write --companies c.yml   # + role-ящики на компанию по домену (N-1.4)
cat /tmp/net.yml | ./bin/onlyoffice/import-network.go --manifest - --write             # stdin
```

Флаги: `--manifest <file|->` (обязателен), `--dry-run` (preview, ничего не
пишет), `--write` (создавать; по умолчанию report-only), `--limit N`
(макс. новых за прогон), `--tag` (default `2dph:network:<target-email>`),
`--backfill`, `--companies <file|->` (оба — N-1.4 #271, идемпотентны).
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
  (`--companies`, N-1.4 #271) линкует их на компанию; matched-существующие
  получают тег/about аддитивно (`--backfill`, N-1.4 #271).

## Идемпотентность

Ключ = email (lowercase). Один проход по CRM (`BuildContactEmailIndex`);
существующий по email (включая импортированных из VCF/MAB #85) — **skip**
(`matched`), не перезаписывается (ручные правки не трогаем). Повторный
прогон = 0 созданий / 0 изменений. При сбое email/тег-прикрепления только
что созданный контакт удаляется (rollback) — «created» = полностью
созданный контакт, повтор не заводит дубль.

## N-1.4 #271: дописывание matched (--backfill) и компания-группировка (--companies)

Шаги N-1.4 выполняются тем же инструментом ПОСЛЕ создания (см. примеры
выше); каждый шаг идемпотентен, report-only/dry-run по умолчанию ничего не
пишет.

### Дописывание существующим matched (`--backfill`)

Решение владельца (#267, вопрос 3): существующий по email контакт (в т.ч.
импорт VCF/MAB #85) **не пропускается молча**, а получает аддитивно:

- тег `2dph:network:<target>` — если у контакта его нет;
- about-premises-сводку из манифеста — **только если about пуст** (ручные
  правки, в т.ч. Org из #85 в about, не перезаписываются).

Механизм по email-ключу от манифеста (не хардкод под id); переиспользуется
N-1.5 #275 для eslider@. Состояние тега — множество id под тегом
(`ListContactsByTag`), about/имена — `GetContact`. Повтор = 0 изменений
(`updated=0 unchanged=N`). Отчёт: `updated / would (dry-run) / unchanged /
missing / failed`.

### Компания-группировка role-ящиков (`--companies`)

Маппинг «домен email → компания» — YAML-файл, напр. `companies-wheregroup.yml`:

```yaml
wheregroup.com: WhereGroup
```

Линкуются **только role-ящики/организации манифеста** (`kind=company`:
alle@, info@, wartung@…), чей домен есть в маппинге: `FindCompany` → если
нет, `CreateCompany` (оба идемпотентны по нормализованному имени) →
`UpdatePerson` с `companyId`. **person-kind не трогаем** (реальные люди — не
«роль-ящик»); company-kind без правила остаётся Person-контактом как есть
(`unmapped`, в пилоте #270 — events@suse). Повтор = 0 изменений (`found`,
`already`, `unmapped`). Отчёт: `found / created / linked / already /
notfound / unmapped / failed` (+ `missing / wouldlink` в dry-run).

Компания ищется по нормализованному display name (без GmbH-суффиксов и
слоганов, go-onlyoffice `CompanyGroupingKey`) — **значение Name в маппинге
должно совпадать с реальной компанией CRM** (анализ — dry-run: found vs
missing); при несовпадении имени dry-run покажет `missing`, и маппинг
правится до `--write` (иначе CreateCompany заведёт дубль юрлица).

## Код и тесты

- `internal/network/manifest.go` — контракт манифеста `network.Manifest`
  (общий у продюсера `bin/network` и коннектора; раньше — crmDoc/crmLink в
  bin/network);
- `internal/ooimport` (cgo-free): `ParseManifest` → `BuildPlan` (маппинг,
  фильтр сервисов, тег, about, kind связи) → `Run` (lookups + создание,
  dry-run/limit); N-1.4: `RunBackfill` (дописывание matched, аддитивно) +
  `RunCompanies` (`ParseCompanies`/`GroupCompanyLinks`, линковка role-ящиков
  на компанию по домену);
- CLI `bin/onlyoffice/import-network.go` (shebang, tag
  `onlyoffice_import_network`);
- тесты: офлайн (`internal/ooimport/*_test.go` — план/маппинг/roundtrip
  продюсер-консьюмер, split по existing: повтор = 0 новых; ParseCompanies,
  GroupCompanyLinks, BackfillChanges аддитивность) + интеграция
  (`//go:build integration`, живой OO, creds из окружения, без creds —
  skip; созданные тестовые контакты удаляются в конце; тег остаётся
  0-count — API клиента не удаляет теги); SplitPersonName-косметика
  (mailconv) — трейлинговая « (Org)»-декорация снимается до разбора имени.

## Ссылки

- Epic N-1 #267; дети: N-1.1 #268 (фильтр), N-1.3 #270 (пилот), N-1.4 #271
  (компании).
- Сеть: docs/brain/graph-network.md; фильтр: docs/brain/crm-network-filter.md.
- go-onlyoffice: примитивы FindPersonByEmail/BuildContactEmailIndex/
  CreatePerson/AddContactInfo/CreateContactTag/AddContactTag.
