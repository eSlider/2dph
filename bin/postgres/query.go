//usr/bin/env go run -tags=postgres_query "$0" "$@"; exit
//go:build postgres_query
//
// bin/postgres/query.go - read-only Postgres as YAML.
//
//	./bin/postgres/query.go --profile onlyoffice -c 'SELECT 1'
//
// Profiles: $HOME/.config/brain/db-profiles.yml (credentials stay out of git).
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/pkg/cmdbin"
)

func main() {
	os.Exit(cmdbin.ExecFile("bin/db/psql-yq", os.Args[1:]))
}
