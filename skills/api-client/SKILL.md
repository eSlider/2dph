# Skill: api-client — canonical Go API client pattern

Use when implementing ANY HTTP/API client, connection pool, streaming
consumer, or client-side data transform in Go. Mandatory before writing such
code (AGENTS.md, code standard #4).

First-class example: https://github.com/eSlider/go-ollama (client.go).
Typed CRUD reference: go-onlyoffice `tasks.go` (`CreateProjectTask`,
`UpdateProjectTask`, `GetTasks`).

## Shape (mandatory)

1. `type Client struct { hc *http.Client; cfg Config }` — one handle; state
   concurrency expectations in the doc comment.
2. Typed `Config`/DSN struct (URL, token, timeouts) resolved from the
   go-config stack (`etc/brain/config.yml` → local → `.env` → env). Never
   hardcoded URLs/tokens; nil-safe constructor `New(cfg *Config) *Client`.
3. One transport core on receiver:
   - `doJSON(ctx, method, path, body, dest)`: sets headers/auth,
     non-2xx → wrapped error incl. body snippet, decodes into typed dest;
   - `doStream(ctx, …)`: NDJSON/SSE variant with `onItem func(T) error`.
   Public methods stay thin:
   `func (c *Client) X(ctx context.Context, req XRequest) (*XResponse, error)`.
4. Per endpoint: `{Name}Request` / `{Name}Response` structs. Optional fields =
   pointers + `omitempty`; wire quirks get custom `MarshalJSON`. No
   `map[string]any` in or out; platform payloads convert at the boundary via
   mapstructure/v2 (`WeakDecode` pattern).
5. Errors: `fmt.Errorf("context: %w", err)`; typed sentinel errors where
   callers branch.

## Concurrency (hard rules)

- Public API synchronous; channels internal only, never in signatures.
- `ctx` first param everywhere; every send/receive selects on `<-ctx.Done()`;
  workers exit on cancel — zero leaked or stuck goroutines (wg-accounted).
- Bounded parallelism: worker pool caps in-flight calls (rate limiting).
- Sender closes channels. Errors ride inside `Result{val, err}` or return
  synchronously — never ignored.
- Fan-out/fan-in to merge multi-source fetches; `errgroup.WithContext` when
  first-error-cancel fits.

Reference implementation (worker pool + rate limit + cancellation):

```go
func FetchConcurrent(ctx context.Context, urls []string, limit int) ([]Result, error) {
	jobs := make(chan string) // unbuffered = natural backpressure
	results := make(chan Result, len(urls))
	var wg sync.WaitGroup
	wg.Add(limit)
	for w := 0; w < limit; w++ {
		go func() {
			defer wg.Done()
			for u := range jobs {
				val, err := doCall(ctx, u)
				select {
				case results <- Result{val: val, err: err}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() { // sender owns close
		defer close(jobs)
		for _, u := range urls {
			select {
			case jobs <- u:
			case <-ctx.Done():
				return
			}
		}
	}()

	out := make([]Result, 0, len(urls))
	for range urls {
		select {
		case r := <-results:
			if r.err != nil {
				wg.Wait()
				return nil, r.err
			}
			out = append(out, r.val)
		case <-ctx.Done():
			wg.Wait()
			return nil, ctx.Err()
		}
	}
	wg.Wait()
	return out, nil
}
```

## Performance (hot paths)

- Transport core reuses read/write buffers via `sync.Pool` (reset in Put).
- Stream-decode responses chunk-wise into typed structs — never buffer whole
  bodies just to unmarshal.
- NDJSON/SSE assembly via pre-grown `strings.Builder`; request bodies built
  once per call, not per retry.

## Testing & checklist before PR

- Offline `httptest.Server` fixtures; no network in unit tests.
- `go vet ./...`; `go test -race ./...`; one test cancels mid-stream and
  asserts workers returned (no goroutine leak).
- No absolute paths/URLs in code; config wired through go-config stack.
- Primitives via `pkg/utils`; errors wrapped with context.
