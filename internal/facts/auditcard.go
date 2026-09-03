package facts

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Vinogradov audit card (L-9.2 #231): каждый вердикт 2dph-аудита фиксируется
// карточкой в SoT audits[] — verdict только на FACT/OPEN- premises.

// Verdict is the audit outcome; only accept/reject/weaken are legal.
type Verdict string

const (
	VerdictAccept Verdict = "accept"
	VerdictReject Verdict = "reject"
	VerdictWeaken Verdict = "weaken"
)

// Valid reports whether v is one of accept|reject|weaken.
func (v Verdict) Valid() bool {
	switch v {
	case VerdictAccept, VerdictReject, VerdictWeaken:
		return true
	}
	return false
}

// Inference is the reasoning step named by Vinogradov (deduction|induction|analogy|other).
type Inference string

const (
	InferenceDeduction Inference = "deduction"
	InferenceInduction Inference = "induction"
	InferenceAnalogy   Inference = "analogy"
	InferenceOther     Inference = "other"
)

// Valid reports whether i is one of deduction|induction|analogy|other.
func (i Inference) Valid() bool {
	switch i {
	case InferenceDeduction, InferenceInduction, InferenceAnalogy, InferenceOther:
		return true
	}
	return false
}

// AuditCard is one entry of the SoT audits[] section (AUD-NNNN).
type AuditCard struct {
	ID        string    `yaml:"id" json:"id"`
	Date      string    `yaml:"date" json:"date"`
	Claim     string    `yaml:"claim" json:"claim"`
	Premises  []string  `yaml:"premises" json:"premises"`
	Inference Inference `yaml:"inference" json:"inference"`
	Gaps      []string  `yaml:"gaps,omitempty" json:"gaps,omitempty"`
	Counter   string    `yaml:"counter" json:"counter"`
	Verdict   Verdict   `yaml:"verdict" json:"verdict"`
}

// ValidateAuditCard returns human-readable problems; empty slice means the card
// may be appended. Every card needs a dated claim, FACT-/OPEN- premises, a
// named inference, a counter (use "none" when absent) and a legal verdict.
func ValidateAuditCard(c AuditCard) []string {
	var ps []string
	if strings.TrimSpace(c.Claim) == "" {
		ps = append(ps, "claim is required")
	}
	if strings.TrimSpace(c.Date) == "" {
		ps = append(ps, "date is required (YYYY-MM-DD)")
	}
	if len(c.Premises) == 0 {
		ps = append(ps, "premises are required (FACT-…/OPEN-…)")
	}
	for i, p := range c.Premises {
		if strings.TrimSpace(p) == "" {
			ps = append(ps, fmt.Sprintf("premises[%d] is empty", i))
		}
	}
	if !c.Inference.Valid() {
		ps = append(ps, "inference must be one of deduction|induction|analogy|other")
	}
	if strings.TrimSpace(c.Counter) == "" {
		ps = append(ps, `counter is required (use "none" when there is no counter)`)
	}
	if !c.Verdict.Valid() {
		ps = append(ps, "verdict must be one of accept|reject|weaken")
	}
	return ps
}

var reAuditID = regexp.MustCompile(`^AUD-(\d+)$`)

// NextAuditID returns the next zero-padded AUD-NNNN id after the given ids.
// Non-AUD ids are ignored; an empty list starts at AUD-0001.
func NextAuditID(ids []string) string {
	max := 0
	for _, id := range ids {
		m := reAuditID.FindStringSubmatch(strings.TrimSpace(id))
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		if n > max {
			max = n
		}
	}
	return fmt.Sprintf("AUD-%04d", max+1)
}

// AppendAuditCard validates c, assigns the next AUD-NNNN id, appends the card
// to audits[] of the SoT yaml at path and rewrites the file. The rest of the
// document (facts/open_questions/docs_index, comments, styles) survives the
// round trip unchanged: the edit is done on the yaml.Node tree, never on a
// typed re-marshal. Returns the card with its assigned ID.
func AppendAuditCard(path string, c AuditCard) (AuditCard, error) {
	if ps := ValidateAuditCard(c); len(ps) > 0 {
		return AuditCard{}, fmt.Errorf("audit card: %s", strings.Join(ps, "; "))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return AuditCard{}, fmt.Errorf("audit card: read SoT %s: %w", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return AuditCard{}, fmt.Errorf("audit card: parse SoT %s: %w", path, err)
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return AuditCard{}, fmt.Errorf("audit card: SoT %s: root is not a mapping", path)
	}
	audits := mappingValue(root, "audits")
	if audits == nil {
		return AuditCard{}, fmt.Errorf("audit card: SoT %s: audits[] section missing", path)
	}
	if audits.Kind != yaml.SequenceNode {
		return AuditCard{}, fmt.Errorf("audit card: SoT %s: audits is not a sequence", path)
	}

	var ids []string
	for _, item := range audits.Content {
		if item.Kind == yaml.MappingNode {
			if id := mappingValue(item, "id"); id != nil {
				ids = append(ids, id.Value)
			}
		}
	}
	c.ID = NextAuditID(ids)
	audits.Content = append(audits.Content, auditCardNode(c))

	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return AuditCard{}, fmt.Errorf("audit card: encode SoT: %w", err)
	}
	if err := enc.Close(); err != nil {
		return AuditCard{}, fmt.Errorf("audit card: encode SoT: %w", err)
	}
	if err := writeFilePreservingMode(path, []byte(b.String())); err != nil {
		return AuditCard{}, fmt.Errorf("audit card: write SoT %s: %w", path, err)
	}
	return c, nil
}

// mappingValue returns the value node for key in a mapping node, or nil.
func mappingValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// auditCardNode renders the card as a mapping node with the canonical key
// order of SoT audits[] entries (id, date, claim, premises, inference, gaps,
// counter, verdict). Date is double-quoted so it stays a string on re-read;
// the emitter picks safe quoting for the remaining free-text scalars.
func auditCardNode(c AuditCard) *yaml.Node {
	m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	key := func(k string) *yaml.Node {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}
	}
	str := func(k, v string) *yaml.Node {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
	}
	seq := func(ss []string) *yaml.Node {
		n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, s := range ss {
			n.Content = append(n.Content, str("", s))
		}
		return n
	}
	add := func(k string, v *yaml.Node) { m.Content = append(m.Content, key(k), v) }

	add("id", str("", c.ID))
	date := str("", c.Date)
	date.Style = yaml.DoubleQuotedStyle
	add("date", date)
	add("claim", str("", c.Claim))
	add("premises", seq(c.Premises))
	add("inference", str("", string(c.Inference)))
	if len(c.Gaps) > 0 {
		add("gaps", seq(c.Gaps))
	}
	add("counter", str("", c.Counter))
	add("verdict", str("", string(c.Verdict)))
	return m
}

// writeFilePreservingMode keeps the SoT file permission bits across rewrites.
func writeFilePreservingMode(path string, data []byte) error {
	perm := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		perm = st.Mode().Perm()
	}
	return os.WriteFile(path, data, perm)
}
