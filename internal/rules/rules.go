// Package rules — типизированный каталог правил вывода (epic #324).
//
// Каталог — сжатая производная форма правил внешнего источника (например,
// MIT 6.042J «Mathematics for Computer Science»): id, domain, name, form
// (premises ⊢ conclusion), краткая формулировка своими словами, ссылка на
// раздел. Лицензированный текст источника здесь НЕ хранится: коммитим только
// сжатие (см. epic #324). Исходник извлечения живёт в gitignored var/.
//
// Пакет cgo-free: только stdlib + gopkg.in/yaml.v3, тестируется без Ladybug.
package rules

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Domain — раздел каталога. Совпадает с крупными блоками источника; для
// 6.042J: proofs (I), structures (II), counting (III), probability (IV),
// recurrences (V).
type Domain string

const (
	DomainProofs      Domain = "proofs"        // логика, методы доказательств
	DomainSets        Domain = "sets"          // множества, отношения, функции, мощности
	DomainInduction   Domain = "induction"     // индукция, рекурсивные типы, state machines
	DomainInfinite    Domain = "infinite-sets" // бесконечные мощности, halting
	DomainNumber      Domain = "number-theory" // делимость, GCD, модулярная арифметика, RSA
	DomainGraphs      Domain = "graphs"        // ориентированные/простые/планарные графы, сети
	DomainCounting    Domain = "counting"      // суммы, асимптотика, cardinality, generating functions
	DomainProbability Domain = "probability"   // вероятностные пространства, отклонения, random walks
	DomainRecurrences Domain = "recurrences"   // рекуррентности, divide-and-conquer
)

// domains — допустимые значения Domain (валидация каталога).
var domains = map[Domain]bool{
	DomainProofs:      true,
	DomainSets:        true,
	DomainInduction:   true,
	DomainInfinite:    true,
	DomainNumber:      true,
	DomainGraphs:      true,
	DomainCounting:    true,
	DomainProbability: true,
	DomainRecurrences: true,
}

// Rule — одно правило каталога. Form — форма вывода (premises ⊢ conclusion);
// Statement — краткая формулировка СВОИМИ словами (не цитата источника).
// Executable помечает правила, которые движок аудита (internal/facts) реально
// применяет; остальные — справочные (каталог + поиск).
type Rule struct {
	ID         string   `yaml:"id" json:"id"`
	Domain     Domain   `yaml:"domain" json:"domain"`
	Name       string   `yaml:"name" json:"name"`
	Form       string   `yaml:"form" json:"form"`
	Statement  string   `yaml:"statement" json:"statement"`
	Source     string   `yaml:"source" json:"source"`
	Executable bool     `yaml:"executable" json:"executable"`
	Tags       []string `yaml:"tags,omitempty" json:"tags,omitempty"`
}

// ruleFile — форма YAML-файла каталога (mapping с ключом rules, чтобы
// оставить место для метаданных файла).
type ruleFile struct {
	Rules []Rule `yaml:"rules"`
}

// idRe — допустимый id правила: точки как namespace
// (domain.family.name), нижний регистр, цифры, дефис.
var idRe = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z0-9][a-z0-9-]*)+$`)

// Index — метаданные каталога (etc/rules/<src>/index.yml): provenance
// источника. Текста источника не содержит — только отпечаток артефакта.
type Index struct {
	Source  string `yaml:"source"`  // URL/edition источника
	License string `yaml:"license"` // лицензия источника (CC BY-NC-SA 3.0)
	SHA256  string `yaml:"sha256"`  // sha256 артефакта извлечения (PDF)
	Pages   int    `yaml:"pages"`
	Edition string `yaml:"edition"`
}

// Load читает каталог из директории: все *.yml/*.yaml, кроме index.yml,
// объединяет правила, валидирует и возвращает отсортированными по ID.
// Пустая директория — не ошибка (вернёт nil): каталог наполняется главами.
func Load(dir string) ([]Rule, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if n == "index.yml" || n == "index.yaml" {
			continue
		}
		if !strings.HasSuffix(n, ".yml") && !strings.HasSuffix(n, ".yaml") {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)

	var all []Rule
	for _, n := range names {
		raw, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			return nil, err
		}
		var f ruleFile
		if err := yaml.Unmarshal(raw, &f); err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
		for i := range f.Rules {
			normRule(&f.Rules[i])
		}
		all = append(all, f.Rules...)
	}
	if err := Validate(all); err != nil {
		return nil, err
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	return all, nil
}

// LoadIndex читает index.yml каталога (provenance). Отсутствие файла —
// ошибка: каталог без источника непроверяем.
func LoadIndex(dir string) (Index, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "index.yml"))
	if err != nil {
		return Index{}, err
	}
	var idx Index
	if err := yaml.Unmarshal(raw, &idx); err != nil {
		return Index{}, fmt.Errorf("index.yml: %w", err)
	}
	return idx, nil
}

// normRule тримит строковые поля (YAML допускает хвостовые пробелы).
func normRule(r *Rule) {
	r.ID = strings.TrimSpace(r.ID)
	r.Name = strings.TrimSpace(r.Name)
	r.Form = strings.TrimSpace(r.Form)
	r.Statement = strings.TrimSpace(r.Statement)
	r.Source = strings.TrimSpace(r.Source)
	r.Domain = Domain(strings.TrimSpace(string(r.Domain)))
}

// Validate проверяет каталог: уникальные id, известный domain, непустые
// name/form/source; executable-правило обязано нести форму вывода (⊢).
// Ошибка называет правило — каталог правится точечно.
func Validate(rs []Rule) error {
	seen := make(map[string]bool, len(rs))
	for i := range rs {
		r := rs[i]
		where := r.ID
		if where == "" {
			where = fmt.Sprintf("#%d", i)
		}
		switch {
		case r.ID == "":
			return fmt.Errorf("rule %s: id is required", where)
		case !idRe.MatchString(r.ID):
			return fmt.Errorf("rule %s: invalid id (want dotted lower-case)", where)
		case seen[r.ID]:
			return fmt.Errorf("rule %s: duplicate id", where)
		case !domains[r.Domain]:
			return fmt.Errorf("rule %s: unknown domain %q", where, r.Domain)
		case r.Name == "":
			return fmt.Errorf("rule %s: name is required", where)
		case r.Form == "":
			return fmt.Errorf("rule %s: form is required", where)
		case r.Source == "":
			return fmt.Errorf("rule %s: source is required", where)
		case r.Executable && !strings.Contains(r.Form, "⊢"):
			return fmt.Errorf("rule %s: executable rule needs a derivation form (premises ⊢ conclusion)", where)
		}
		seen[r.ID] = true
	}
	return nil
}

// Executable возвращает только правила, применимые движком аудита.
func Executable(rs []Rule) []Rule {
	var out []Rule
	for _, r := range rs {
		if r.Executable {
			out = append(out, r)
		}
	}
	return out
}

// ErrNoRules — каталог пуст (наполнение ещё не начато); удобно для CLI.
var ErrNoRules = errors.New("rules: catalog is empty")
