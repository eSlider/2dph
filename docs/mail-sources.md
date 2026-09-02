---
type: reference
status: current
related:
  - PLAN.md
  - docs/runbook.md
---

# Почта + контакты на /mnt/8TB — источник-инвентарь (issue #79)

Таблица-истина: какие дисковые источники почты/контактов на /mnt/8TB учтены в
пайплайне импорта, какие ещё заблокированы. Сам импорт-конвейер: mbox/.eml →
`internal/mailconv` → `message.json`/`message.md` (корпус `var/corpus/mail`),
контакты — через `oow catalog scan-contacts` + reconcile CRM (#68).

## Источники

| Источник | Путь (под /mnt/8TB) | Формат | Статус импорта |
|---|---|---|---|
| TB-профиль Andriy (старый ImapMail) | `contacts/Andriy Oblivantsev/emails/Thunderbird/Profilordner/{Mail,ImapMail}` | mbox-дерево | импортировано (wave-1): `tb-andriy-profile` |
| Outlook PST Andriy | `contacts/Andriy Oblivantsev/MS Office Outlook.pst` | pst | импортировано (#185): `pst-andriy` |
| .eml россыпью по персонам | `contacts/<Person>/**.eml` | eml | импортировано (wave-1): `contacts-eml`, 125 файлов |
| Backup 128Gb: E-Mails dir | `admin/Backups/Backup 128 Gb Ubuntu 20.02/E-Mails/{andriy.oblivantsev@gridfactor.de,Local Folders,pska2160@gmail.com,viscreation@gmx.de}` | TB mbox | импортировано (wave-1): `tb-backup-128g` |
| Backup 128Gb: zip 2010 | `.../E-Mails/E-Mails(2010-07-07 18-35).zip` | TB mbox внутри zip | импортировано (wave-1): `tb-2010-zip` (распаковка во временный root) |
| PST в бэкапе | `.../Documents/Vorlagen/MS Office Outlook.pst` | pst | импортировано (#185): `pst-backup-128g-vorlagen` (байт-идентичен PST Andriy) |
| VCF по персонам | `contacts/<Person>/**.vcf` + `Telegram Desktop/*.vcf` | vcf | частично: `oow catalog scan-contacts` |
| VCF пачкой | `contacts/Contacts VCF's/contacts-*.vcf` | vcf | частично: `oow catalog scan-contacts` |
| TB адресные книги | `Profilordner/{abook.mab,history.mab}` | Mork (.mab) | парсер есть: `pkg/contact/mab.go` |

Исключения политики (НЕ импортируем, #79): `Aleksey Krylov` (чужая
переписка), Drafts/Templates/Trash/Junk/Spam/Unsent (включая немецкие имена
Outlook: `Entwürfe`/`Vorlagen`/`Gelöschte Objekte`/`Junk-E-Mail`/`Postausgang`),
`*.msf`, `filterlog.html`, `soft/drivers/**/*.pst` (прошивки).

## mbox-импорт

Сплиттер живёт в `internal/mailconv` (`SplitMbox`, `SplitMboxDir`) — единая
реализация (правило #10), тестируемая оффлайн; `bin/mail/convert-mbox.go` —
тонкая CLI-обёртка. Вывод контент-адресуемый
(`<out>/<source>/<rel-dir>/<sha256:16>/<sha256:16>.eml`), поэтому повторный
прогон идемпотентен (повторов нет, как seen-set в #74).

```bash
./bin/mail/convert-mbox.go --in DIR --out var/corpus/mail --dry-run   # счёт
./bin/mail/convert-mbox.go --in DIR --out var/corpus/mail             # импорт
./bin/mail/import.go --from-eml var/corpus/mail                       # eml → md/json
```

source-теги: `tb-backup/<acct>`, `tb-profile/<folder>` — через `--source` или
топ-уровневую директорию под `--in`.

## PST-импорт (issue #185)

`bin/mail/import-pst.go` (тонкая CLI-обёртка; оркестрация в
`internal/source` — адаптер `source.PST` + `ImportPST`/`PlanPST`) читает
секцию `pst.*` типизированного конфига: список `sources: [{label, path}]`
(пути — из инвентаря #79, класть в `config.local.yml`), опциональные
`readpst` (бинарник), `out` (корпус, по умолчанию
`var/corpus/mail/pst`), `state` (чекпойнт, по умолчанию `var/state/pst.json`).

Для каждого источника: `readpst -e <file.pst> -o var/tmp/pst/<label>` (scratch
вытирается перед каждым прогоном — readpst не идемпотентен на непустой
выходной каталог), затем каждый извлечённый `.eml` копируется контент-адресно
в `var/corpus/mail/pst/<label>/<folder>/<sha256:16>/<sha256:16>.eml` и дерево
конвертируется через общий `mailconv.FromEML` (source-тег `pst/`). Идемпотентность:
sha256 seen-set в `var/state/pst.json` (driver `source.Sync`, паттерн #97/#98)
+ контент-адресуемый вывод. readpst вставляет в MIME-boundary случайный токен
`LibPST-iamunique-<n>` на каждом прогоне — адаптер нормализует его, чтобы
контент-ID были стабильны (иначе повторный прогон плодил бы дубли).

Политика #79 применяется и здесь: папки Drafts/Templates/Trash/Junk/Spam/Unsent
(немецкие имена тоже) не импортируются (`mailconv.SkipFolder`).

```bash
./bin/mail/import-pst.go --dry-run    # план без записи
./bin/mail/import-pst.go              # импорт (идемпотентно)
```

readpst (пакет `pst-utils` в Ubuntu 24.04, бывший `libpst-utils`): на хосте без
root-доступа устанавливается локально в `var/dist/readpst` (dpkg-deb -x), путь
задаётся через `pst.readpst`. Результат по обоим PST (файлы байт-идентичны —
второй это копия из бэкапа): 1 уникальное письмо (Outlook-приветствие 2005) в
`Posteingang`, 78 контактов VCF + 6 событий ICS извлечены readpst, но
импортируются другими пайплайнами (#68); исключённых папок нет (пустые).

## Mail-инкубатор: legacy `.eml` → docker-mailserver (эпик #250, issue #252)

Ревизионная зона почты: ETL из legacy-корпусов (`var/mail`, 37GB) идёт **через
mail-server** (docker-mailserver, `info@` читает ящики как shared). Автосинк в
gator `kind=mail` — эпик B (вне A).

**Модель ящика (решение 2026-09-02, #252):** ящик инкубатора = **исторический
адрес владельца** периода, НЕ абстрактный «источник». Гдеgroup-период →
`andriy.oblivantsev@wheregroup.com`; на будущие периоды — свой аккаунт на
каждый исторический адрес (`eslider@gmail.com`, `…@viscreation.de`). Письма
раскладываются по адресату: owner в `From` → **Sent**; owner в
`To`/`Cc`/`Delivered-To` → **INBOX**; owner нигде (чужое/тикет-рассылки,
не-распарсенные) → **INBOX/Unmatched** (карантин, разбирается отдельно, не
выкидывается молча). Служебные папки аккаунта — стандартные (Sent/Drafts/
Trash/Junk, как у обычного ящика). A1-ящик `wheregroup@produktor.io`
(абстрактный источник) — свернутая модель: письма переложены в
`andriy.oblivantsev@wheregroup.com`, аккаунт удалён (#252).

`bin/mail/incubator.go` (тонкая CLI-обёртка; оркестрация в `internal/incubator`
— `Run`/`Scan`/`MailboxOfDir`/`LayoutOf`) читает секцию `incubator.*`
типизированного конфига: `imports: [{label, source, user, owner, state}]`
(пути корпусов — из инвентаря #79, класть в `config.local.yml`), `docker`,
`container`.

Пайплайн на источник (label = owner; гдеgroup → `andriy.oblivantsev@wheregroup.com`):

1. **Scan**: обход `**/*.eml` профиля TB (числовые id-директории = письма;
   копии под `attachments/` пропускаются — в гдеgroup это единственный .eml без
   Message-ID).
2. **Канон**: Message-ID из заголовка (stdlib `net/mail`, header-only): первый
   токен, trim `<>`, lowercase — контрактный ключ gator `kind=mail` (эпик B).
   Письма без Message-ID → fallback `body-sha256:<hex>` тела, помечаются в
   логе. (emersion/go-message для этого НЕ используется: он падает на legacy
   charset `iso-8859-15` в заголовках гдеgroup — регрессия зафиксирована при
   первом live-прогоне.)
3. **Раскладка** (при заданном `owner`): `LayoutOf` — owner в From → Sent,
   owner в To/Cc/Delivered-To → INBOX, иначе INBOX/Unmatched. Адрес-списки
   парсятся (addr-spec, регистронезависимо); при не-парсящемся значении —
   substring-fallback, чтобы legacy-письмо атрибутировалось владельцу.
   Без `owner` — legacy-режим: плоско в INBOX или дерево папок (`--folders`).
4. **Дедуп/идемпотентность**: манифест `var/state/incubator-<label>.json`
   (Message-ID канон → путь + папка) — источник истины. Повторный прогон с тем
   же `--limit` даёт 0 новых. `doveadm save` сам **не** дедуплицирует
   (проверено live, 2026-09-02) — на doveadm не полагаемся. Чекпойнт пишется
   атомарно каждые 25 импортов (crash-resume). `--force` пересоздаёт манифест
   (перекладка после смены аккаунта/модели).
5. **Импорт**: `docker exec -i mailserver doveadm save -u <user> -m <mb>` с
   .eml на stdin — **bind-mount legacy-корпуса в контейнер не нужен** (решение
   открытого вопроса #250/Q3). Целевые папки создаются `doveadm mailbox
   create` (doveadm save не автосоздаёт папку — проверено live); после импорта
   в новые папки (INBOX/Unmatched, --folders-дерево) — повторный прогон
   user-patches.sh (права info@ на новые папки).

```bash
./bin/mail/incubator.go --dry-run               # скан + план (папки, счёт)
./bin/mail/incubator.go --limit 1000            # 1000 писем с раскладкой по owner из конфига
./bin/mail/incubator.go --owner <addr> --limit 1000  # раскладка по явному адресу
./bin/mail/incubator.go --limit 1000 --folders  # legacy: дерево папок (без owner)
./bin/mail/incubator.go --limit 1086 --force    # пере-импорт окна с пересозданием манифеста
```

Результат пилота (2026-09-02, модель «исторический адрес»): найдено 4249
писем-сообщений (4250 .eml на диске, из них 1 — attachment-копия
`0000640/attachments/ForwardedMessage.eml`, пропущена), уникальных Message-ID
**3986** (263 дубля в корпусе); в `andriy.oblivantsev@wheregroup.com`
переложено 1000 (Sent/INBOX/INBOX-Unmatched — раскладка в отчёте #252);
повторный прогон → 0 новых. Проверка: `doveadm search -u
andriy.oblivantsev@wheregroup.com mailbox INBOX ALL` (счёт).
