# Graph network: «кто с кем и через кого» из mail+commits (L-9.5 #234)

Дизайн сетевого слоя поверх готового графа kb.lbug (mail D-1 #257: Message/
Person + SENT/TO/CC/BCC/REPLY_TO; git L-9.4 #233: Commit/Person/AUTHORED).
Цель эпика L-9 (#229, ADR-0012 §сеть связей): восстановление социально-
профессиональной сети — «с кем и через кого я работал, над чем, когда» — для
CRM/маркетинга. Инструмент read-only: граф не пишет, схему не меняет.

## 1. Деривации (только из существующих узлов/рёбер)

Для целевого Person (email) выводятся связи с другими Person:

| Канал | Деривация | Из узлов/рёбер | Вес |
|-------|-----------|----------------|-----|
| mail direct | письмо, где target — sender и Q — получатель, или наоборот | SENT, TO/CC/BCC | TO 1.0 / CC 0.5 / BCC 0.25 |
| mail shared | письмо, где target и Q оба — получатели (sender — третий) | TO/CC/BCC | +0.25 |
| mail reply | письмо Q отвечает на письмо target (и наоборот) | REPLY_TO | +0.5 |
| mail thread | число thread_id, где оба участвуют | Message.thread_id | контекст |
| git project | target и Q оба AUTHORED коммиты в один repo | Commit.repo, AUTHORED | отдельный факт |

Person.id = email lowercase (канон mail D-1 #257; git-авторы сопряжены по
email в L-9.4 #233) — тот же email в mail и git = один Person-узел.

## 2. Premises и verdict (audit card по Vinogradov, ADR-0012)

Каждый факт связи несёт:

- **premises** — ссылки на Message.id (mail) / Commit.id `repo:sha` (git);
  без premises связь не выводится (sufficient reason, ADR-0012 п.1);
- **verdict**:
  - `accept` — ≥2 прямых писем, или ≥1 REPLY_TO-диалог, или общий проект
    (обе стороны AUTHORED в repo); экспортируется в CRM;
  - `weaken` — одиночный контакт (1 письмо без ответа и без проекта) или
    только общий получатель; gap `OPEN`, в CRM-экспорт НЕ идёт (ADR-0012
    п.4 «только проверенные связи»; п.2 «1 письмо ≠ устойчивая связь»);
- **inference** = `deduction`; **период** = min..max дат свидетельств.

## 3. Код

- `internal/network/network.go` (cgo-free): `BuildLinks(Rows, Filter)` — вся
  деривация, детерминированная (сортировка по весу, tie-break email).
- `internal/network/query.go` (cgo, read-only): `LoadRows(conn)` — читает
  Message/Person/рёбра + Commit/AUTHORED в строки сети.
- `bin/network/network.go` (cgo, read-only): CLI.

```bash
bin/network/network.go --person eslider@gmail.com
bin/network/network.go --person eslider@gmail.com --json
bin/network/network.go --person alice@x --project demo --since 2026-01-01
bin/network/network.go --person alice@x --accept-only     # CRM-экспорт YAML
```

Флаги: `--person EMAIL` (обязателен), `--project REPO` (git-ось: только
связи с общим проектом), `--since/--until`, `--limit`, `--json`,
`--accept-only` (YAML только accept, без дублей — одна запись на связь,
агрегат mail+git каналов), `--db PATH` (default `<root>/var/kb.lbug`).
`--depth` зарезервирован (пилот #234 — только прямые связи, depth=1).

## 4. Экспорт в CRM

`--accept-only` → YAML: target + список accept-связей (person, name, msgs,
threads, replies, period, projects[], premises[] — первые 5 + extraPremises
счётчик; полный список в `--json`). Дублей нет: каждая связь один раз со
всеми каналами. Источник помечен `source: 2dph graph mail+git (L-9.5 #234)`.

## 5. Границы (что НЕ выводится)

- «что именно обсуждали» — по телу письма (Paragraph/PART_OF не
  материализованы, D-1.1 YAGNI);
- транзитивные цепочки «знакомые знакомых» — depth>1 вне пилота #234;
- алиасы email (один человек, разные адреса) — граф сопрягает строго по
  email (L-9.4 #233), alias-резолв — отдельная задача;
- mail ↔ repo привязка (письмо «о проекте X») — в графе нет такого ребра,
  не выдумывается.

## Ссылки

- Epic L-9 #229; тело L-9.5 #234 (пилот/приёмка).
- D-1 #257 (mail-граф), L-9.4 #233 (Commit/Person/AUTHORED, git-facts).
- ADR-0012 §сеть связей (premises/verdict/экспорт accept), ADR-0013.
- docs/facts/audit-recipes.md (шаблон audit card).
