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
| Outlook PST Andriy | `contacts/Andriy Oblivantsev/MS Office Outlook.pst` | pst | **ЗАБЛОКИРОВАНО** (нет `readpst`) |
| .eml россыпью по персонам | `contacts/<Person>/**.eml` | eml | импортировано (wave-1): `contacts-eml`, 125 файлов |
| Backup 128Gb: E-Mails dir | `admin/Backups/Backup 128 Gb Ubuntu 20.02/E-Mails/{andriy.oblivantsev@gridfactor.de,Local Folders,pska2160@gmail.com,viscreation@gmx.de}` | TB mbox | импортировано (wave-1): `tb-backup-128g` |
| Backup 128Gb: zip 2010 | `.../E-Mails/E-Mails(2010-07-07 18-35).zip` | TB mbox внутри zip | импортировано (wave-1): `tb-2010-zip` (распаковка во временный root) |
| PST в бэкапе | `.../Documents/Vorlagen/MS Office Outlook.pst` | pst | **ЗАБЛОКИРОВАНО** (нет `readpst`) |
| VCF по персонам | `contacts/<Person>/**.vcf` + `Telegram Desktop/*.vcf` | vcf | частично: `oow catalog scan-contacts` |
| VCF пачкой | `contacts/Contacts VCF's/contacts-*.vcf` | vcf | частично: `oow catalog scan-contacts` |
| TB адресные книги | `Profilordner/{abook.mab,history.mab}` | Mork (.mab) | парсер есть: `pkg/contact/mab.go` |

Исключения политики (НЕ импортируем, #79): `Aleksey Krylov` (чужая
переписка), Drafts/Templates/Trash/Junk/Spam/Unsent, `*.msf`, `filterlog.html`,
`soft/drivers/**/*.pst` (прошивки).

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

## PST — блокер

`readpst -e` (libpst) НЕ установлен на хосте (проверено 2026-08-25). PST-файлы
небольшие (265 KB оба), но без конвертера их содержимое не извлекается. Путь
закрытия (#79, close-out п.2): установить `libpst-utils` → `readpst -e -o <out>
<file>.pst` (по одному `.eml` на сообщение) → тот же пайплайн `mailconv.FromEML`
(source `pst/`). До установки источник остаётся в графе как **ЗАБЛОКИРОВАНО**.
