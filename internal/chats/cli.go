package chats

import (
	cliparse "github.com/eSlider/2dph/internal/cli"
	"github.com/integrii/flaggy"
)

type syncTelegramFlags struct {
	Limit int
	Phone string
}

type syncLinkedInFlags struct {
	Limit   int
	Refresh bool
}

func SyncParser() *flaggy.Parser {
	p := cliparse.New("chats-sync")
	p.Description = "download chats to var/chats"
	tg := flaggy.NewSubcommand("telegram")
	li := flaggy.NewSubcommand("linkedin")
	var limit int
	var phone string
	var refresh bool
	tg.Int(&limit, "", "limit", "max messages per chat")
	tg.String(&phone, "", "phone", "phone (default TELEGRAM_PHONE)")
	li.Int(&limit, "", "limit", "max messages per conversation")
	li.Bool(&refresh, "", "refresh", "refresh webtop session")
	p.AttachSubcommand(tg, 1)
	p.AttachSubcommand(li, 1)
	return p
}

func ImportParser() *flaggy.Parser {
	return cliparse.New("chats-import")
}

func FactsParser() *flaggy.Parser {
	return cliparse.New("chats-facts")
}

func ApplyParser() *flaggy.Parser {
	p := cliparse.New("chats-apply")
	dry := false
	p.Bool(&dry, "", "dry-run", "show without writing")
	return p
}

func parseTelegramFlags(args []string) (syncTelegramFlags, error) {
	var f syncTelegramFlags
	p := cliparse.New("chats-sync-telegram")
	p.Int(&f.Limit, "", "limit", "max messages per chat")
	p.String(&f.Phone, "", "phone", "phone (default TELEGRAM_PHONE)")
	return f, cliparse.Parse(p, args)
}

func parseLinkedInFlags(args []string) (syncLinkedInFlags, error) {
	var f syncLinkedInFlags
	p := cliparse.New("chats-sync-linkedin")
	p.Int(&f.Limit, "", "limit", "max messages per conversation")
	p.Bool(&f.Refresh, "", "refresh", "refresh webtop session")
	return f, cliparse.Parse(p, args)
}

func parseApplyFlags(args []string) (dryRun bool, err error) {
	p := cliparse.New("chats-apply")
	p.Bool(&dryRun, "", "dry-run", "show without writing")
	return dryRun, cliparse.Parse(p, args)
}

func parseNoFlags(name string, args []string) error {
	return cliparse.Parse(cliparse.New(name), args)
}
