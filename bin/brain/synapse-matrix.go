//usr/bin/env bash -c 'exec "${0%/*}/../cgo/zig" go run -tags=system_ladybug,brain_synapse "$0" "$@"' "$0" "$@"; exit
//go:build cgo && system_ladybug && brain_synapse
//
// bin/brain/synapse-matrix.go - expose the brain (leafs + edges) as a service
// for pc-agent. Metaphor: leafs = neurons, SYNAPTIC edges = synapses (issue #82).
//
//	./bin/brain/synapse-matrix.go                     # bind 127.0.0.1:8632 (loopback only)
//	KB_SYNAPSE_PORT=8633 ./bin/brain/synapse-matrix.go
//	KB_SYNAPSE_HOST=0.0.0.0 KB_SYNAPSE_TOKEN=... ./bin/brain/synapse-matrix.go
//
// Auth (#82): when synapse.token is set every route except /health requires
// `Authorization: Bearer <token>`; without a token the service refuses to bind
// anything but a loopback address. Config via the go-config stack
// (etc/brain/config.yml -> local -> .env -> env), see internal/config.
//
// NOTE: never run gofmt -w (breaks the shebang).
package main

import (
	"context"
	"log"

	"github.com/eSlider/2dph/internal/brain"
	"github.com/eSlider/2dph/internal/config"
	"github.com/eSlider/2dph/pkg/httpapi"
)

func main() {
	cfg, err := config.Load(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	brain.Configure(cfg)
	if err := brain.Ready(); err != nil {
		log.Fatal(err)
	}
	httpapi.RunSynapse(brain.HTTP{}, cfg)
}
