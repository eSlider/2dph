package facts

// Interval of truth for a fact leaf (D24 / OQ5). Not D16 source staleness.
//
// Empty valid_from and valid_to means "always" (legacy leafs). Empty asOf
// means "do not filter". Dates compare as YYYY-MM-DD (lexicographic).

// NormalizeDay keeps the calendar day from ISO-8601 or bare dates.
func NormalizeDay(s string) string {
	s = trimSpace(s)
	if len(s) >= 10 && s[4] == '-' && s[7] == '-' {
		return s[:10]
	}
	return s
}

func trimSpace(s string) string {
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\n' || s[j-1] == '\r') {
		j--
	}
	return s[i:j]
}

// ActiveAt reports whether a fact with [validFrom, validTo] holds at asOf.
// validTo empty = open-ended. Both ends inclusive.
func ActiveAt(validFrom, validTo, asOf string) bool {
	asOf = NormalizeDay(asOf)
	if asOf == "" {
		return true
	}
	from := NormalizeDay(validFrom)
	to := NormalizeDay(validTo)
	if from != "" && asOf < from {
		return false
	}
	if to != "" && asOf > to {
		return false
	}
	return true
}
