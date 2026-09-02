# Brain corpus rebuild (runbook)

Optimized corpus (re)index. Parallel embedding, batched writes, resume, a live
progress/ETA monitor, and cross-chunk dedup before write (issue #248).

## Commands

Build the native binaries (also runs the cgo brain tests):

    scripts/stack/serve-brain --build        # or ./bin/cgo/zig go build ...

Fresh rebuild of info + mail + facts with control flags (одна команда, без
двухфазного workaround-а — пул для FTS-фазы подбирается автоматически, #244):

    bin-build/brain-index \
        --rebuild --with-mail --with-facts \
        --workers 12 --batch 256 --progress 5 --skip

Corpus adapters (P-9.3): `--with-mail` → mail-адаптер, `--with-chats` →
chats-адаптер, `--corpus DIR` → docs-адаптер (доп. пути), `--git-root DIR` →
git-адаптер (история репозиториев). Каждый корпус — `contract.Source`
(`internal/corpus`); index пишет их единым `WriteCorpus`.

> Facts require `--with-facts` (or `--facts-json`). Corpus leafs are always
> written as `root=info` (single file source each); the facts layer
> (`root=facts`, ≥2 independent sources) comes from `bin/facts/extract.go` and
> is only ingested on rebuild when the flag is set. A rebuild **without**
> `--with-facts` leaves the facts-root empty even though all leafs are
> `confidence=confirmed` (#181). To repair an existing DB, see
> `docs/facts/ingest.md` (`bin/facts/extract.go` + `bin/facts/promote.go`).

- `--workers N`   parallel embedding goroutines (default 4; use ~cores on the box)
- `--batch N`     leafs per upsert transaction (default 64)
- `--progress N`  print rate + ETA every N seconds to stderr
- `--skip`        resume: skip leafs whose id is already in the db

### Dedup перед записью (issue #248 A1)

`WriteCorpusChunked` дедуплицирует **кросс-чанково и всегда** (не только при
`--skip`): leaf с контрактным `ContentHash` (source|external_id|kind|text),
уже виденным в этом прогоне, не эмбеддится и не пишется. Это убирает двойной
стрим одной почты (live `var/corpus/mail` + legacy `var/mail`, #199/#184):
полный rebuild — 307k streamed → ~105k unique, C-сторона делает 3× меньше
MERGE/undo-работы. Детерминизм id/порядка финальных leafs не меняется
(дубликаты схлопывались бы к тому же id и на C-стороне — теперь раньше).

Pass 1 (`CountCorpus`, dry-run) считает так же: `info` = уникальных
(сколько ляжет в БД), `streamed` = сырой стрим с дублями. `--limit` считается
по стримнутым leafs, поэтому dry-run и фактическая запись при том же limit
дают одно число.

Because leaf ids are deterministic (`contract.ContentHash`, P-9.3), `--skip`
makes a re-run cheap: it filters existing ids before embedding, so it embeds
only new leafs. After a partial/aborted run, re-running with `--skip` skips the
already-written corpus and goes straight to index build.

### Embedding-колонка и ANN (issue #248 B3)

Когда `vector.ann.enabled=true` (прод-дефолт #206), rebuild **не пишет
embedding-колонку**: модель не грузится вовсе (минус ~1.5GB RSS write-фазы),
leafs и facts пишутся текстом. Векторный индекс строит шаг волны `ann-build`
(`bin/brain/ann.go ensure`) из текста напрямую — `extractRows` эмбеддит
column-less leafs моделью (legacy-БД с колонкой идут старым путём, без
модели). Колонка пишется только когда ANN **выключен** — тогда векторный
путь поиска это linear-scan по `l.embedding` (search.go `vecScanStmt`).

> Note: `--rebuild` deletes the db, so `--skip` + `--rebuild` always restarts
> fresh. To resume a crashed build (leafs already written, indexes missing),
> run `--skip` **without** `--rebuild` so the db is preserved and only missing
> indexes are built — фаза индексов в обоих случаях одна и та же
> (`BuildIndexes`, автопул):
>
>     bin-build/brain-index --skip \
>         --with-mail --with-facts --workers 12 --batch 256 --progress 5

> Warning: `--rebuild` refuses to run while a brain holds the db open (fd
> holders via /proc, or an API answering on `127.0.0.1:$KB_PORT` when
> rebuilding the repo-default `var/kb.lbug`) — deleting the file under a live
> serve leaves it serving the removed inode and can lock the service out of
> the fresh db. Stop/restart the brain first, pass `--force` to override, or
> set `KB_INDEX_ALLOW_LIVE=1` in environments where the swap+restart flow is
> intended (the compose `index` service already sets this).

Observed rates: embedding is fast (~16k/s, 256-dim); the db **write** phase is
the bottleneck (~100/s, ~40 min for 242k leafs).

Before a fresh rebuild, remove the old db (daemons must be down first):

    scripts/stack/serve-brain --stop          # or pkill bin-build/brain-*
    rm -f var/kb.lbug var/kb.lbug.wal

## Buffer pool (память, issue #244)

Две фазы прогона живут на разных пулах:

- **write-фаза** (запись leafs) — на конфигурированном `KB_BUFFER_POOL`
  (default 1 GB). Явный `KB_BUFFER_POOL` остаётся нижней границей для обеих фаз.
- **FTS-фаза** (`CREATE_FTS_INDEX`, после записи leafs) — хэндл закрывается,
  БД переоткрывается с автоподобранным пулом `max(1GB, chars×32)`, где chars —
  суммарный размер текста корпуса (`MATCH (l:Leaf) RETURN sum(size(l.text))`).
  Замеры порога «buffer pool full»: реальные mail-лифы 10.3 MB текста →
  64 MB падает / 128 MB проходит; синтетика 10.8 MB → 192 MB / 256 MB;
  продакшн 105k лифов (280 MB текста) → 1 GB падает / 10 GB проходит.
  Коэффициент ×32 даёт ~9 GB на полном корпусе.

Пул — это page cache, а не преаллокация: RSS растёт только под реальный
working set, поэтому завышение пула безопасно (память не резервируется).

### C-потолок: write-фаза ~140 KB/leaf (замер #244)

Помимо пула, C-сторона Ladybug держит ~135-145 KB на каждый записанный лиф
(независимо от пула: при 1 GB пуле и 30k лифов RSS вырос до ~6.3 GB;
эмбеддинг не влияет — замер без `embedding` дал те же 141 KB/leaf).
Рост линейный и **не освобождается при закрытии хэндла в том же процессе**
(malloc-арены): полный корпус 105k → ~14-15 GB C-side + модель ~1.5 GB.
CHECKPOINT каждые 1024 лифа не влияет. Это внутренности liblbug 0.19.1
(undo/версионные структуры узла), вне Go-контроля.

> Checkpoint (issue #248 B1, замерено): liblbug 0.19.1 включает
> `auto_checkpoint` по умолчанию с порогом WAL 16MB — и go-ladybug v0.17.0
> пробрасывает это как есть (поля `auto_checkpoint`/`checkpoint_threshold` в
> Go-`SystemConfig` отсутствуют, toC стартует от C-дефолта). В write-фазе WAL
> растёт до ~16MB и циклически чекпойнтится в основную БД (наблюдалось на
> 15k leafs: wal 0→16.6MB → сброс в 2.2MB, db растёт порциями). Явный
> `CHECKPOINT` как Cypher-стейтмент тоже принимается. На RSS это не влияет
> (рост дают undo-структуры, не WAL) — главный рычаг пика это dedup (A1):
> 3× меньше MERGE/undo на C-стороне.

Следствие для пикового RSS: фаза индексов стартует после закрытия write-хэндла
(тот же процесс — память удержана), поэтому пик ≈ C-накопление write-фазы +
working set FTS-фазы. Раньше (до #244) runbook предписывал
`KB_BUFFER_POOL=10GB` на весь прогон — write-фаза дополнительно раздувала
dirty-страницы к 10 GB и FTS строился на том же хэндле поверх накопленного:
это и давало наблюдаемые 39-41 GB. Теперь write-фаза идёт на 1 GB пуле, FTS —
на автопуле после закрытия хэндла.

Если нужен пик ниже (отдельные процессы для фаз полностью освобождают C-память
между прогонами — двухфазный workaround) — это контроль DevOps на полном
прогоне; код даёт корректный результат и в одном процессе.

### C-потолок: орфан-таблица (почему автопул)

При падении `CREATE_FTS_INDEX` (нехватка пула) Ladybug оставляет частичную
внутреннюю таблицу `0_id_appears_info` (строки токенов до точки отказа).
Она недостижима через `DROP TABLE`/`DROP_FTS_INDEX` (каталог скрывает
внутренние таблицы; проверено на liblbug 0.19.1 и в исходниках Kuzu FTS:
`appears_info` создаётся первой, дропается в конце rewrite-запроса) и
навсегда блокирует повторный `CREATE_FTS_INDEX` на этой БД. Поэтому пул
фазы индексов подбирается заранее; если FTS всё же упал (хосту не хватает
RAM), ошибка явно называет орфан и recovery: удалить БД и пересобрать или
восстановить бэкап. Критерий #237 «пик RSS ≤ 2-4 GB» формально относится к
Go-части (чанкинг, выполнено); C-сторона требует мультигигабайты и на write
(~140 KB/leaf), и на FTS (пул ~9 GB на полном корпусе) — это осознанные
C-ограничения Ladybug (issue #244).

`--skip-indexes` по-прежнему пишет только leafs (первая фаза workaround-а
для отладки/внешних индексов); в обычном прогоне он не нужен.

## Control / monitoring

`ProgressReporter` (cgo-free, `internal/brain/corpus_pool.go`) prints
`index: done/total rate/s eta=...` to stderr at `--progress` interval. The worker
pool (`parallelEmbed`) is order-preserving, respects `ctx` cancellation, and
collects embed errors per-result.

## After the index-build phase

    scripts/stack/serve-brain                 # builds + starts brain-serve + brain-search

Verify:

    bin-build/brain-search ... 17830      # or the API :8630, sort-by-date
