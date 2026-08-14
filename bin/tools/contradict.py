"""D16 contradiction adjudication (same rules as internal/facts)."""
from __future__ import annotations

from typing import Any

CONF_CONFIRMED = "confirmed"
CONF_HYPOTHESIS = "hypothesis"

RULE_UNRESOLVED = "unresolved"
RULE_TEMPORAL = "temporal_freshness"
RULE_AUTHORITY = "authority_pairing"
RULE_TWO_SOURCE = "two_source"
RULE_SINGLE = "single_source"

KIND_RUNTIME = "runtime"
KIND_CONFIG = "config"
KIND_NARRATIVE = "narrative"


def _independent(sources: list[dict]) -> int:
    seen: set[str] = set()
    for i, s in enumerate(sources):
        sid = str(s.get("id") or "") or f"{s.get('kind', '')}#{i}"
        seen.add(sid)
    return len(seen)


def _fresh_n(sources: list[dict]) -> int:
    return sum(1 for s in sources if not s.get("stale"))


def _strong_n(sources: list[dict]) -> int:
    return sum(1 for s in sources if s.get("kind") in (KIND_RUNTIME, KIND_CONFIG))


def adjudicate(claim: dict[str, Any]) -> dict[str, Any]:
    yes = list(claim.get("yes") or [])
    no = list(claim.get("no") or [])
    yes_n, no_n = _independent(yes), _independent(no)
    text = str(claim.get("text") or "")

    def out(conf: str, rule: str, winner: str = "") -> dict[str, Any]:
        return {
            "text": text,
            "confidence": conf,
            "confirmed": conf == CONF_CONFIRMED,
            "rule": rule,
            "winner": winner,
            "yes": yes_n,
            "no": no_n,
        }

    if yes_n < 2 or no_n < 2:
        if yes_n >= 2:
            return out(CONF_CONFIRMED, RULE_TWO_SOURCE, "yes")
        if no_n >= 2:
            return out(CONF_CONFIRMED, RULE_TWO_SOURCE, "no")
        return out(CONF_HYPOTHESIS, RULE_SINGLE)
    yf, nf = _fresh_n(yes), _fresh_n(no)
    if yf >= 2 and nf < 2:
        return out(CONF_CONFIRMED, RULE_TEMPORAL, "yes")
    if nf >= 2 and yf < 2:
        return out(CONF_CONFIRMED, RULE_TEMPORAL, "no")
    ys, ns = _strong_n(yes), _strong_n(no)
    if ys >= 2 and ns < 2:
        return out(CONF_CONFIRMED, RULE_AUTHORITY, "yes")
    if ns >= 2 and ys < 2:
        return out(CONF_CONFIRMED, RULE_AUTHORITY, "no")
    return out(CONF_HYPOTHESIS, RULE_UNRESOLVED)


def parse_source_field(source: str) -> tuple[str, str]:
    """Split `a x b vs c x d` into (yes, no). Empty no if no ` vs `."""
    if " vs " not in source:
        return source, ""
    yes, _, no = source.partition(" vs ")
    return yes.strip(), no.strip()


def check_fact_row(lid: str, source: str, loc: str, how: str, conf: str) -> list[str]:
    """Lexicon checks for one facts leaf (no Ladybug)."""
    problems: list[str] = []
    src = source or ""
    if conf == CONF_CONFIRMED:
        if " vs " in src:
            problems.append(f"{lid}: confirmed fact cannot keep a vs-contradiction")
        if " x " not in src:
            problems.append(f"{lid}: needs 2-source evidence in source, got '{source}'")
    elif conf == CONF_HYPOTHESIS:
        yes, no = parse_source_field(src)
        if not no or " x " not in yes or " x " not in no:
            problems.append(
                f"{lid}: hypothesis contradiction needs 'a x b vs c x d', got '{source}'"
            )
    elif conf == "partial":
        pass
    else:
        problems.append(f"{lid}: unknown confidence '{conf}'")
    if not loc:
        problems.append(f"{lid}: missing loc (evidence pointer)")
    if not how:
        problems.append(f"{lid}: missing how")
    return problems
