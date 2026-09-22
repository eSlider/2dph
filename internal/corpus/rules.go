package corpus

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eSlider/2dph/internal/contract"
	"github.com/eSlider/2dph/internal/rules"
	"github.com/eSlider/2dph/pkg/utils"
)

// Rules — адаптер корпуса каталога правил (etc/rules, epic #324 P5).
//
// Один leaf на правило: kind=reference, source=rules,
// external_id=<catalog>/<rule-id>. Текст — самодостаточная карточка правила
// (name + form + statement + source + executable), пригодная для поиска в
// brain. Парсинг и валидация — через internal/rules (единый источник схемы).
// Каталог без index.yml пропускается; отсутствующий etc/rules — не ошибка.
// Пакет cgo-free.
type Rules struct {
	Root string // repo root; пусто → utils.Root()
	Dir  string // каталог каталогов; пусто → <root>/etc/rules
}

func (r Rules) Name() string { return "rules" }

func (r Rules) Stream(ctx context.Context, emit func(contract.Leaf) error) error {
	root := r.Root
	if root == "" {
		root = utils.Root()
	}
	dir := r.Dir
	if dir == "" {
		dir = filepath.Join(root, "etc", "rules")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // каталога нет — корпус просто пуст
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		catalog := e.Name()
		cdir := filepath.Join(dir, catalog)
		if _, err := rules.LoadIndex(cdir); err != nil {
			continue // не каталог правил (нет index.yml)
		}
		rs, err := rules.Load(cdir)
		if err != nil {
			return fmt.Errorf("rules %s: %w", catalog, err)
		}
		for _, rule := range rs {
			if err := emit(ruleLeaf(catalog, cdir, rule)); err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}

// ruleLeaf — карточка правила: текст самодостаточен для поиска (имя, форма,
// смысл, источник, флаг исполняемости).
func ruleLeaf(catalog, cdir string, rule rules.Rule) contract.Leaf {
	kind := "reference"
	usage := "reference (retrieval)"
	if rule.Executable {
		usage = "executable (audit engine)"
	}
	text := strings.Join([]string{
		rule.Name,
		"form: " + rule.Form,
		rule.Statement,
		"source: " + rule.Source,
		"domain: " + string(rule.Domain),
		"usage: " + usage,
	}, "\n")
	return contract.Leaf{
		Source:     "rules",
		ExternalID: catalog + "/" + rule.ID,
		Kind:       kind,
		Text:       text,
		Root:       "info",
		Confidence: "confirmed",
		How:        "rules/catalog",
		Loc:        cdir,
	}
}
