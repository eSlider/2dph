//usr/bin/env go run -tags=brain_client "$0" "$@"; exit
//go:build brain_client
//
// bin/brain/client.go - сервисный клиент read-контракта brain (P-9.5):
// единый способ для агентов/скриптов читать факты, info и аудит через
// контракт (HTTP :8630, bin/brain/serve.go), без прямого доступа к kb.lbug.
//
//	./bin/brain/client.go search "q" [--root facts|info] [-n N] [--as-of D] [--no-web]
//	./bin/brain/client.go get <id> [--body]
//	./bin/brain/client.go stats
//	./bin/brain/client.go audit
//	common: [--json] [--base URL] [--token T]
//
// Гейт facts: search --root facts возвращает только подтверждённые факты
// (confirmed); отклонённое (info, hypothesis/partial — в т.ч. 2v2-противоречия)
// уходит в not_confirmed с пометкой (not confirmed). audit с не-confirmed
// confidence на facts-корне выходит с кодом 1 — такие листы нельзя подавать
// как факты, пока их не разберёт audit (bin/brain/audit-contract.go).
//
// Реализация: pkg/brainclient (SDK, cgo-free) + pkg/brainclient/cli (логика
// подкоманд, покрыта тестами в go test ./...). Base по умолчанию — из
// internal/config (host/port), иначе 127.0.0.1:8630.
//
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/pkg/brainclient/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
