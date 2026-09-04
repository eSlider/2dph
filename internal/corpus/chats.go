package corpus

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/eSlider/2dph/internal/contract"
	"github.com/eSlider/2dph/internal/markdown"
	"github.com/eSlider/2dph/pkg/utils"
)

// Chats — адаптер chat-корпуса: messages.md под var/corpus/chats/md
// (telegram/linkedin/whatsapp exports). source=chats,
// external_id=frontmatter id (первый message id), kind=тип из frontmatter.
type Chats struct {
	Root string // repo root; пусто → utils.Root()
	Dir  string // каталог messages.md; пусто → <root>/var/corpus/chats/md
}

func (c Chats) Name() string { return "chats" }

func (c Chats) Stream(ctx context.Context, emit func(contract.Leaf) error) error {
	if c.Dir == "" {
		if c.Root == "" {
			c.Root = utils.Root()
		}
		c.Dir = filepath.Join(c.Root, "var", "corpus", "chats", "md")
	}
	if st, err := os.Stat(c.Dir); err != nil || !st.IsDir() {
		return nil
	}
	return filepath.Walk(c.Dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext != ".md" && ext != ".markdown" {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		meta, _ := markdown.ExtractFrontmatter(string(raw))
		externalID := meta["id"]
		kind := meta["type"]
		if kind == "" {
			kind = "chat"
		}
		for _, lf := range markdown.ToAll(string(raw), p, "chats") {
			text := lf.Heading + "\n\n" + lf.Text
			if lf.Heading == "" {
				text = lf.Text
			}
			if externalID == "" {
				externalID = contentAddr(text) // #5.2: единый content-address fallback
			}
			if err := emit(contract.Leaf{
				Source: "chats", ExternalID: externalID, Kind: kind,
				Text: text, Root: "info", Confidence: "confirmed",
				How: "kb/index", Loc: p,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}
