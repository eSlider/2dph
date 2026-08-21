// Commands in this directory are shebang mains (sync.go, import.go, facts.go,
// apply.go), each behind an exclusive build tag so `go build ./bin/chat`
// does not see two mains. Shared code lives in internal/chat.
package main
