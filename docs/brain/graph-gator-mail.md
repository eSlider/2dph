# Graph: gator kind=mail → 2dph Message/Person (D-1, ADR-0013)

Дизайн-документ коннектора и переноса conversation-канона (#99) в Ladybug.
D-1.1 (#258). Цель: 2dph строит граф Message/Person/рёбра из канона gator
kind=mail, а не из сырого корпуса var/mail (ADR-0013).

## 1. Вход: gator kind=mail (канон почты)

- **Где**: `/mnt/8TB/projects/produktor/gator/var/gator/parquet/mail/source=mail/channel={wheregroup,gmail}/dt=*/data_*.parquet`
  (hive-каталог на том же хосте arc-01; 2dph и gator — общий /mnt/8TB).
- **Каналы живые**: `wheregroup` (3984 unique, andriy.oblivantsev@wheregroup.com),
  `gmail` (18007, eslider@gmail.com).
- **Чтение**: `query.Mail` (gator internal/query/mail.go) — SQL над parquet
  через `MailFilter` (latest per message_id по умолчанию; `History=true` все
  версии; `Limit` default ~100 → нужна пагинация). Либо прямой
  `read_parquet(glob, union_by_name=true)` — быстрее на полный канал, минуя
  gator-пагинацию. Коннектор 2dph читает **только канон gator** (parquet/mail),
  НЕ сырьё `var/mail`.
- **Tombstone**: `deleted=true` (G-9.3) — latest по умолчанию исключает
  удалённое. 2dph при повторном sync должен учитывать: письмо удалённое в
  Roundcube → исключается из графа (или помечается), History видит удалённое.

## 2. Маппинг row gator → canon.Message

Row gator kind=mail (колонки query.Mail, согласованы в G-9.2 #89):

| gator row | canon.Message (internal/canon #99) | заметка |
|---|---|---|
| message_id | Message.ID | уже канон Message-ID (CanonMessageID/Fallback) |
| folder | — (атрибут) | поле на узле, не в рёбрах |
| from {name,email} | Message.From (Person) | gator уже разобрал — MIME-парсинг НЕ нужен |
| to/cc/bcc []{name,email} | Message.To/CC/BCC ([]Person) | как выше |
| in_reply_to [] | Message.ReplyTo / ThreadID | см. §3 |
| date (RFC3339) | Message.SentAt | |
| subject | — (атрибут) | на узле, если нужен |
| body | Message.Body | переработанное тело (без цитат) |
| content_hash | gator_ref | deeplink `kind=mail#v-<hash8>` |

Person.ID для mail = lowercased email (как canon Person), Name — display name
из gator.

## 3. Thread_id / REPLY_TO

- gator read-схема не имеет References — только `in_reply_to` (список Message-ID,
  на которые отвечает). 
- Стратегия треда: узел Message несёт `thread_id`. Корень = письмо без
  in_reply_to (или с пустым). REPLY_TO-ребро: `(m)-[:REPLY_TO]->(parent)` по
  in_reply_to. Тред-группировка для Person↔Project↔Time (L-9.5 #234) строится
  транзитивно по REPLY_TO (клиент сети связей), не материализуется отдельным
  узлом Thread на первом этапе (YAGNI до запроса).
- fallback: письма без in_reply_to и без References → root-сообщение
  (thread_id = message_id).

## 4. Перенос #99 в Ladybug-схему

- **Модель/рёбра**: `canon.Person`/`canon.Message`/`Message.Edges()` (SENT/TO/
  CC/BCC/REPLY_TO/PART_OF) — переиспользуются **as-is** (уже в main, golden-тест).
- **Ladybug-схема** (InitSchema, internal/brain/write.go) — аддитивно добавить:
  - `Message` node table (id STRING PK = message_id; thread_id, folder,
    subject, sent_at, gator_ref, body, ...);
  - rel tables `SENT (FROM Person TO Message)`, `TO/CC/BCC (FROM Message TO
    Person)`, `REPLY_TO (FROM Message TO Message)`;
  - `Person` node table — УЖЕ ЕСТЬ (id/name/email), не создавать повторно;
    заполнить из gator.
  - Paragraph/PART_OF (из canon.Edges) — на первом этапе НЕ включать (AD-решение:
    граф сопряжения Person/Message первичен; парсинг тел на Paragraph — позже
    при запросе семантики, YAGNI).
- **Запись**: Ladybug `MERGE` по id (идемпотентно, как WriteCorpus). Не
  store.go JSON-манифест #99 — роль берёт Ladybug.
- **Инкремент**: повторный sync идемпотентен (MERGE); tombstone → пометка
  узла/исключение.

## 5. Коннектор (архитектура)

```
gator parquet/mail (channel X) --read_parquet/query.Mail--> []canon.Message
    └── canon.Message.Edges() ──► Ladybug MERGE (Person/Message + рёбра)
```

- Новый `bin/mail/graph.go` (или расширение существующего import-пути):
  читает канал → canon.Message → MERGE в kb.lbug.
- Путь чтения: прямой `read_parquet` (полный канал без gator-пагинации) —
  предпочтителен для 3984/18007; query.Mail — для точечных/инкрементальных.
- Конфиг: каналы из gator (как registry), hive-путь.

## 6. Что из #99 НЕ переносится на gator-путь

- `frommail.go` (emersion/go-message MIME-парсинг) — gator уже разобрал from/
  to/cc/bcc/body; зависимость не тянуть, дубль не писать.
- `store.go` (JSON-манифест на диск) — заменён Ladybug MERGE.
- `fromchat.go` — чаты отдельным каналом позже (вне D-1), модель едина.

## 7. Deliverables / следующий шаг (D-1.2)

- Схема Message/рёбра в InitSchema + write-путь (MERGE) — TDD.
- Импортёр гдеgroup (3984) → пилот → gmail (18007).
- Оценка размера/времени: 18k писем ~ простой MERGE (секунды-минуты).

## Ссылки

- ADR-0013 (mail canon → gator; 2dph = Message/Person/Leaf граф)
- ADR-0012 (§сеть связей), domain-model.md (Message/Person #99)
- gator: G-9 (#87), query.Mail (#89), tombstone (#90), каналы гдеgroup/gmail
- 2dph: P-9.6 #242 (ADR памяти — ссылается, не дублирует), L-9.4 #233
