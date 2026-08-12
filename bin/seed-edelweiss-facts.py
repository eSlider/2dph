#!/usr/bin/env python3
"""Seed confirmed Edelweiss facts via pairing (a x b). Never DROP INDEX."""
from __future__ import annotations

import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "bin" / "tools"))

import yaml
from kblib import (  # noqa: E402
    CONF_CONFIRMED,
    ROOT_FACTS,
    connect,
    ensure_indexes,
    leaf_index_names,
    stats,
    upsert_leaf,
)
from model2vec import StaticModel  # noqa: E402

LEX = Path("/home/devops/projects/edelweiss-curasoft/docs/lexicon/edelweiss-lexicon.yml")
CS = Path("/home/devops/projects/curasoft/docs/lexicon/cs-lexicon.yml")


def fact(text: str, source: str, how: str, loc: str) -> tuple[str, str, str, str]:
    return text, source, how, loc


def from_edelweiss_lex(lex: dict) -> list[tuple[str, str, str, str]]:
    rows: list[tuple[str, str, str, str]] = []
    gl = lex["hosts"]["gl"]
    rows.append(fact(
        "CuraSoft GL server VM IP is %s on Fritz LAN (%s)." % (gl["ip"], gl["role"]),
        gl["evidence"], "lexicon hosts.gl", "edelweiss-lexicon.yml",
    ))
    for key in ("pdl", "pdl2", "spdl", "gf"):
        h = lex["hosts"][key]
        rows.append(fact(
            "Edelweiss host %s IP %s role=%s." % (key.upper(), h["ip"], h["role"]),
            h["evidence"], "lexicon hosts.%s" % key, "edelweiss-lexicon.yml",
        ))
    pg, ms = lex["ports"]["postgres"], lex["ports"]["mssql"]
    rows.append(fact(
        "GL LAN DB: PostgreSQL port %s open=%s; MSSQL %s reachable=%s."
        % (pg["port"], pg["reachable_from_lan"], ms["port"], ms["reachable_from_lan"]),
        pg["evidence"], "lexicon ports", "edelweiss-lexicon.yml",
    ))
    b = lex["brain_2dph"]
    rows.append(fact(
        "Edelweiss 2dph pilot brain at %s; facts need >=2 sources. CLI: %s."
        % (b["path"], b["cli"]),
        b["evidence"], "lexicon brain_2dph", "edelweiss-lexicon.yml",
    ))
    v = lex["password_vault"]
    rows.append(fact(
        "Password vault pilot: %s; OO task #%s deadline %s."
        % (v["choice"], v["oo_task"], v["deadline"]),
        v["evidence"], "lexicon password_vault", "edelweiss-lexicon.yml",
    ))
    a = lex["assistant_pilot"]
    rows.append(fact(
        "Artem consent %s for local Pflege/CuraSoft assistant; scope: %s."
        % (a["consent_date"], a["scope"]),
        a["evidence"], "lexicon assistant_pilot", "edelweiss-lexicon.yml",
    ))
    return rows


def from_cs_lex(cs: dict) -> list[tuple[str, str, str, str]]:
    """Item 1: cs-lexicon → facts only with paired evidence."""
    rows: list[tuple[str, str, str, str]] = []
    src = cs.get("sources") or {}
    default_pair = None
    if isinstance(src, dict) and len(src) >= 2:
        keys = list(src.keys())[:2]
        default_pair = "%s x %s" % (keys[0], keys[1])
    ep = cs.get("ep_typ") or {}
    for code, meta in ep.items():
        if not isinstance(meta, dict):
            continue
        ev = (meta.get("evidence") or "").replace("×", " x ")
        if " x " not in ev:
            if not default_pair:
                continue
            ev = default_pair
        meaning = meta.get("meaning") or meta.get("name") or ""
        rows.append(fact(
            "CuraSoft ep_typ %s = %s (%s)." % (code, meta.get("name"), meaning),
            ev, "cs-lexicon ep_typ", "cs-lexicon.yml",
        ))
    pair = "cs-lexicon.yml x curasoft-de/kbs"
    besuch = cs.get("besuch") or {}
    for code, label in besuch.items():
        if code == "note" or not isinstance(label, str):
            continue
        rows.append(fact(
            "CuraSoft Besuch slot %s = %s." % (code, label),
            pair, "cs-lexicon besuch", "cs-lexicon.yml",
        ))
    bm = cs.get("binary_map") or {}
    if isinstance(bm, dict) and bm:
        rows.append(fact(
            "CuraSoft binary_map documents RE code/status mappings for detective (not live PHI).",
            "cs-lexicon.yml x curasoft-detective skill",
            "cs-lexicon binary_map", "cs-lexicon.yml",
        ))
    return rows


def from_oo_and_interview() -> list[tuple[str, str, str, str]]:
    """Items 4+5+6: OO, QEMU/GL, interview bullets via pairing."""
    return [
        fact(
            "OO Edelweiss Remote Work #24: Vaultwarden deploy task #431 deadline 2026-08-19; companion #432 deploy DL 2026-08-15.",
            "OO#431 x 07-interview-2026-08-12.md",
            "OO calendar/tasks pair", "office.produktor.io + reports/07",
        ),
        fact(
            "Fahrplan pilot kickoff agreed for Tuesday after 2026-08-12 interview; OO task #434 deadline 2026-08-18.",
            "OO#434 x 07-interview-2026-08-12.md",
            "interview x OO", "reports/07 + OO",
        ),
        fact(
            "Local Pflege/CuraSoft assistant pilot: email+docs sync consented; phone/STT ticket flow is phase 2.",
            "07-interview-2026-08-12.md x edelweiss-lexicon.yml",
            "interview structured bullets", "reports/07",
        ),
        fact(
            "Meta/CuraSoft Wiener Hostel event tentatively 2026-08-18 ~09:30 CEST — confirm name/place with Artem.",
            "07-interview-2026-08-12.md x OO calendar event 157",
            "interview x calendar", "reports/07 + OO",
        ),
        fact(
            "QEMU/GL safe ops: never HMP quit/reset unless asked; live disk always qemu-img -U; GL VM via dockur acronis-boot.",
            "gl-vm-access.md x qemu-monitor-safe rule",
            "QEMU/GL access pair", "edelweiss-curasoft/docs + .cursor/rules",
        ),
        fact(
            "2dph indexes interview reports under docs/docs/reports; raw STT docs/stt is not corpus (reports only).",
            "2dph-edelweiss-pilot.md x 00-summary.md",
            "STT policy pair", "pilot doc + reports",
        ),
    ]


def main() -> int:
    lex = yaml.safe_load(LEX.read_text())
    cs = yaml.safe_load(CS.read_text()) if CS.exists() else {}
    model = StaticModel.from_pretrained("minishlab/potion-multilingual-128M")
    db, conn = connect(read_only=False)

    rows: list[tuple[str, str, str, str]] = []
    rows.extend(from_edelweiss_lex(lex))
    rows.extend(from_cs_lex(cs))
    rows.extend(from_oo_and_interview())

    for text, source, how, loc in rows:
        if " x " not in source:
            print("skip (no pair)", how, text[:60])
            continue
        emb = model.encode([text])[0].astype(float).tolist()
        upsert_leaf(
            conn,
            text=text,
            root=ROOT_FACTS,
            confidence=CONF_CONFIRMED,
            source=source,
            source_rev="2026-08-12",
            how=how,
            loc=loc,
            type_="fact",
            embedding=emb,
        )
        print("ok", how)

    ensure_indexes(conn)
    print("INDEXES", sorted(leaf_index_names(conn)))
    print(stats(conn))
    conn.close()
    db.close()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
