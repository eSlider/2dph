# Brain corpus rebuild (runbook)

Optimized corpus (re)index. Parallel embedding, batched writes, resume, and a
live progress/ETA monitor.

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

Because leaf ids are deterministic (`contract.ContentHash`, P-9.3), `--skip`
makes a re-run cheap: it filters existing ids before embedding, so it embeds
only new leafs. After a partial/aborted run, re-running with `--skip` skips the
already-written corpus and goes straight to index build.

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
