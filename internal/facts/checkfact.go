package facts

import (
	"fmt"
	"strings"
)

const ConfPartial = "partial"

// ParseSourceField splits `a x b vs c x d` into (yes, no). Empty no when
// there is no ` vs ` marker.
func ParseSourceField(source string) (yes, no string) {
	if i := strings.Index(source, " vs "); i >= 0 {
		return strings.TrimSpace(source[:i]), strings.TrimSpace(source[i+4:])
	}
	return strings.TrimSpace(source), ""
}

// CheckFactRow runs the two-source lexicon checks for one facts leaf.
// It mirrors the former python bin/tools/contradict.check_fact_row.
func CheckFactRow(lid, source, loc, how, conf string) []string {
	var problems []string
	src := source
	switch conf {
	case ConfConfirmed:
		if strings.Contains(src, " vs ") {
			problems = append(problems, lid+": confirmed fact cannot keep a vs-contradiction")
		}
		if !strings.Contains(src, " x ") {
			problems = append(problems, fmt.Sprintf("%s: needs 2-source evidence in source, got '%s'", lid, source))
		}
	case ConfHypothesis:
		yes, no := ParseSourceField(src)
		if no == "" || !strings.Contains(yes, " x ") || !strings.Contains(no, " x ") {
			problems = append(problems, fmt.Sprintf("%s: hypothesis contradiction needs 'a x b vs c x d', got '%s'", lid, source))
		}
	case ConfPartial:
		// partial evidence is expected to be incomplete
	default:
		problems = append(problems, fmt.Sprintf("%s: unknown confidence '%s'", lid, conf))
	}
	if loc == "" {
		problems = append(problems, lid+": missing loc (evidence pointer)")
	}
	if how == "" {
		problems = append(problems, lid+": missing how")
	}
	return problems
}