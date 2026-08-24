//go:build cgo && system_ladybug

//usr/bin/env bash -c 'exec "${0%/*}/../cgo/zig" go run -tags=system_ladybug "$0" "$@"' "$0" "$@"; exit
//
// bin/brain/seed-ext.go - pre-cache Ladybug extensions (FTS, VECTOR) into
// $HOME/.lbdb/extension so the brain can INSTALL/LOAD them offline and on a
// read-only filesystem (the API image runs read_only with HOME=/data).
//
// The compose brain service mounts nothing over ~/.lbdb/extension, so the
// extensions baked into the image are used directly.
//
//	HOME=/data ./bin/brain/seed-ext.go
//
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"fmt"
	"os"

	lbug "github.com/LadybugDB/go-ladybug"
)

func loadExt(conn *lbug.Connection, name string) error {
	res, _ := conn.Query("INSTALL " + name)
	if res != nil {
		res.Close()
	}
	res, err := conn.Query("LOAD EXTENSION " + name)
	if res != nil {
		res.Close()
	}
	return err
}

func main() {
	db, err := lbug.OpenInMemoryDatabase(lbug.DefaultSystemConfig())
	if err != nil {
		fmt.Fprintf(os.Stderr, "seed-ext: open db: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	conn, err := lbug.OpenConnection(db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "seed-ext: open conn: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	for _, ext := range []string{"FTS", "VECTOR"} {
		if err := loadExt(conn, ext); err != nil {
			fmt.Fprintf(os.Stderr, "seed-ext: %s: %v\n", ext, err)
			os.Exit(1)
		}
		fmt.Println("seeded", ext)
	}
}
