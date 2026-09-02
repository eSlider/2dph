# CRM-network filter: люди/компании vs сервис-аккаунты (N-1.1 #268)

Детерминированный классификатор адреса сети связей (L-9.5 #234): **какие
accept-связи — реальные деловые контакты (люди/компании), а какие —
сервис-аккаунты/подсистемы/рассылки**, чтобы в OnlyOffice CRM не попали
GitLab/PayPal/LinkedIn/markets-platform/chiliproject@trac и аналоги.

Эпик N-1 #267 («граф → OO CRM»): N-1.1 (этот фильтр) → N-1.2 коннектор →
N-1.3 пилот гдеgroup → N-1.4 компания-группировка.

## Классы

| kind | Значение | В CRM |
|------|----------|-------|
| `person` | человек: личный адрес или display name = реальное имя | да |
| `company` | организация/роль-ящик (`info@`, `kundenservice@`, отдел, группа) | да (N-1.4 группирует по домену) |
| `service` | автомат/сервис-домен/подсистема/трекер/список рассылки | **нет** |

## Где живёт

- `internal/mailconv/classify.go` (cgo-free): `ClassifySender(ParsedAddress)`
  → `person|company|service`. База — sender-эвристики mailconv
  (`IsMachineSender`, `junkDomains`/`junkLocalParts`, messages.go):
  доменный junk-список переиспользуется через общий `isJunkDomain` (одна
  реализация, правило #10). Сеть НЕ использует локальные junk-ПОДСТРОКИ
  mailconv («info@», «reply», «news») намеренно — см. «Отличия от
  IsMachineSender».
- `internal/network/network.go`: поле `Link.Kind` заполняется в `BuildLinks`
  для каждой связи (обратная совместимость: поле аддитивно, старый вывод
  не ломается).
- `bin/network/network.go`: `--exclude-services` (общий фильтр) и
  `--accept-only` (CRM-экспорт) **исключают `kind=service`**; kind печатается
  в YAML/JSON/text.

## Каскад правил (первое совпадение)

1. Пустой email / без `@` → `service` (защита).
2. **Платформенные релеи людей** (LinkedIn InMail): `hit-reply` /
   `inmail-hit-reply` @ `linkedin.com` → `person`. Display name — реальный
   человек (Ruby Rodriguez, Johanna Eckerstorfer), письма идут через релей
   платформы, а не с личного адреса. Стоит ДО junk-доменов (linkedin.com —
   junk). Дайджест-релеи (`messaging-digest-noreply@…`) — `service`.
3. **Сервис**:
   - junk-домен mailconv (`isJunkDomain`: linkedin.com, github.com,
     google.com, facebook.com, amazon.com, slack.com и др.) или поддомен;
   - сервис-домены сети `networkServiceDomains` (по реальным спискам #268 +
     очевидные аккаунт-платформы): `gitlab.com`, `paypal.com`, `paypal.de`,
     `markets-platform.com`, `xing.com`, `djinni.co`, `wellfound.com`,
     `workablemail.com`, `coinbase.com`, `dynadot.com`, `hetzner.com`,
     `netlify.com`, `letsencrypt.org`, `steampowered.com`, `instagram.com`,
     `discord.com`, `microsoft.com`, `apple.com`;
   - локальная часть-автомат (суффиксы после снятия `._-`): `noreply`,
     `donotreply`, `mailerdaemon`, `postmaster`, `mailrobot`, `mailer`,
     `robot`, `bounce(s)`, `newsletter`, `notifications`, `jobalert(s)`,
     `security`, `digest`, `alerts`; токены: `news`, `marketing`,
     `subscribe`;
   - подсистема/трекер: tool-имя в локальной части или левом лейбле домена
     (`toolNames`: gitlab, projeqtor, chiliproject, trac, redmine, jira,
     confluence, wiki, mediawiki, apache, svn, jenkins, mailman, sympa,
     nagios, zabbix) — ловит `gitlab@wheregroup.com`, `projeqtor@wheregroup.com`,
     `chiliproject@trac.wheregroup.com`, `apache@wiki.wheregroup.com`,
     `gitlab@gitlab`, `dev@trac.example.com`;
   - списки рассылки: локальная часть на `-request`/`-owner`, левый лейбл
     `lists` (`mapbender_dev-request@lists.osgeo.org`).
4. **Person**: display name похож на имя человека — ≥2 токена после снятия
   декораций (« (Орг)», « via X»): Title-case / ALLCAPS / инициалы / частицы
   (van/von/de/of/…); без корпоративных маркеров (GmbH/AG/e.V./Ltd/Inc/…) и
   ролевых слов (Team/Wartung/Neuigkeiten/Digest/…).
5. **Person**: локальная часть `firstname.lastname@домен` (инициал.фамилия
   тоже), display name пуст/неинформативен.
6. Иначе → `company` (роль/организация). Консервативно: НЕ исключаем —
   потеря реального контакта хуже, чем лишняя роль.

## Спец-исключения и границы (OPEN)

- **Домен компании-контекста ≠ сервис**: `wheregroup.com` НЕ входит в
  сервис-домены — коллеги (`astrid.emde@wheregroup.com`) и роль-ящики
  (`info@wheregroup.com` → company) проходят. Подсистемы компании на том же
  домене ловятся tool-именами/лейблами (`gitlab@wheregroup.com`,
  `trac.wheregroup.com`).
- **Отличия от `IsMachineSender`**: локальные junk-подстроки mailconv
  («info@», «reply», «news» как подстрока) в сети не применяются — иначе
  `info@компания` (роль-ящик) и релеи людей (`hit-reply@linkedin.com`)
  терялись бы. IsMachineSender для reconcile-инструментов не изменён.
- **Торговые/потребительские компании** (amazon.de, ebay, банки, телекомы,
  airbnb, opodo и т.п.) классифицируются `company` (у них товарно-денежные
  отношения с пользователем, но это не «сервис-аккаунт»); отсев таких
  доменов из CRM-импорта — отдельное решение владельца (N-1.2/N-1.4).
- **Корпоративные домены гигантов** (google.com, microsoft.com, apple.com и
  др.) — `service`: если реальный человек с такого домена станет деловым
  контактом — добавляется точечное исключение.
- **Люди через релеи**: адрес — релей платформы (`messenger@webex.com`,
  `hit-reply@linkedin.com`), не личный; контакту присваивается класс по
  display name.
- **Двухсловные бренды**, выглядящие как имена («BMW Deutschland») → person
  по форме; точечные ошибки документируются в таблице на issue.
- **Алиасы самого пользователя** (eslider@gmail.com в сети гдеgroup-таргета)
  — проблема алиасов, вне скоупа (#267 YAGNI).

## Применение

```bash
bin/network/network.go --person andriy.oblivantsev@wheregroup.com --accept-only   # CRM YAML, без сервисов
bin/network/network.go --person andriy.oblivantsev@wheregroup.com --exclude-services
bin/network/network.go --person eslider@gmail.com --json                          # полный аудит + kind
```

## Ссылки

- Epic N-1 #267; тело/приёмка N-1.1 #268; сеть L-9.5 #234 (docs/brain/graph-network.md).
- `mailconv.IsMachineSender` (internal/mailconv/messages.go), `mailconv.SplitPersonName`.
