//usr/bin/env go run "$0" "$@"; exit
//
// bin/chat/refresh-session.go - refresh LinkedIn MCP session from webtop CDP.
//
//	./bin/chat/refresh-session.go [--cdp URL] [--root DIR]
//	                                [--container work-webtop] [--profile thorium-profile]
//
// Reads live cookies out of the running Thorium browser via CDP
// (Network.getAllCookies), copies the browser profile, rewrites cookies.json +
// source-state.json. Run before every linkedin sync.
package main

import (
	"fmt"
	"os"

	"github.com/eSlider/2dph/internal/chat"
	cliparse "github.com/eSlider/2dph/pkg/cli"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	var (
		cdp       string
		root      string
		container string
		profile   string
	)
	p := cliparse.New("chat-refresh-session")
	p.Description = "refresh LinkedIn session from webtop CDP (cookies.json + source-state.json)"
	p.String(&cdp, "", "cdp", "CDP endpoint (default http://127.0.0.1:9222)")
	p.String(&root, "", "root", "portable profile root (default /var/tmp/liprofile)")
	p.String(&container, "", "container", "webtop container (default work-webtop)")
	p.String(&profile, "", "profile", "browser profile dir (default thorium-profile)")
	if err := cliparse.Parse(p, args); err != nil {
		return cliparse.Fail(err)
	}
	if err := chat.RefreshLinkedInSession(chat.RefreshLinkedInSessionOpts{
		CDP: cdp, Root: root, Container: container, Profile: profile,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "chat-refresh-session: %v\n", err)
		return 1
	}
	return 0
}
