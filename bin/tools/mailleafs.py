"""Mail markdown under var/mail → info leafs. Conversion stays off the brain DB."""
from __future__ import annotations

import json
from pathlib import Path

from mdleaves import read_markdown, to_all


def msg_date(md: Path) -> str:
    j = md.parent / "message.json"
    try:
        d = json.loads(j.read_text(encoding="utf-8"))
        return (d.get("receivedDate") or d.get("receivedAt") or "")[:10]
    except (OSError, json.JSONDecodeError, TypeError):
        return ""


def from_mail_root(root: Path, limit: int = 0, since: str = "", repo: str = "ooMail") -> list[dict]:
    if not root.is_dir():
        return []
    mds = sorted(root.rglob("message.md"))
    if since:
        mds = [m for m in mds if msg_date(m) >= since]
    if limit:
        mds = mds[:limit]
    leafs: list[dict] = []
    for md in mds:
        files = [md] + sorted((md.parent / "attachments").glob("*.md"))
        for f in files:
            if not f.exists():
                continue
            for lf in to_all(read_markdown(f), f, repo=repo):
                lf["source"] = f"ooMail:{md.parent.name}:{f.name}"
                lf["how"] = "mail/import"
                leafs.append(lf)
    return leafs
