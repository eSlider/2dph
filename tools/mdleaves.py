import json
import re
from pathlib import Path

import mistune


def extract_frontmatter(text: str) -> tuple[dict, str]:
    """Return (frontmatter dict, body). Accepts leading --- yaml ---."""
    if not text.startswith("---"):
        return {}, text
    end = text.find("\n---", 3)
    if end == -1:
        return {}, text
    fm = text[3:end].strip()
    body = text[end + 4 :]
    meta: dict = {}
    for line in fm.splitlines():
        if ":" in line:
            key, _, value = line.partition(":")
            meta[key.strip()] = value.strip().strip("\"'")
    return meta, body


def split_leafs(meta: dict, body: str) -> list[dict]:
    """Split a markdown body into leaf chunks on H2 (##) boundaries.

    Each leaf keeps the document-level frontmatter (type, related) and gets
    its own heading + text. H1 is treated as document title, prepended to the
    first chunk.
    """
    title = ""
    lines = body.splitlines()
    headers: list[tuple[str, int]] = []
    for i, line in enumerate(lines):
        if re.match(r"^# \S", line):
            title = line.lstrip("#").strip()
        elif re.match(r"^## \S", line):
            headers.append((line.lstrip("##").strip(), i))
    if not headers:
        text = "\n".join(l for l in lines if l.strip())
        return [{"heading": title, "text": text.strip()}]

    leafs: list[dict] = []
    for idx, (heading, start) in enumerate(headers):
        end = headers[idx + 1][1] if idx + 1 < len(headers) else len(lines)
        chunk = "\n".join(l for l in lines[start:end] if l.strip())
        text = chunk
        if idx == 0 and title:
            text = f"{title}\n\n{chunk}"
        leafs.append({"heading": heading, "text": text.strip()})
    return leafs


def to_all(text: str, path: str | Path, repo: str = "") -> list[dict]:
    meta, body = extract_frontmatter(text)
    meta.setdefault("type", "reference")
    meta.setdefault("status", "current")
    path = str(path)
    leafs = split_leafs(meta, body)
    out = []
    for lf in leafs:
        out.append({
            "source": path,
            "repo": repo,
            "heading": lf["heading"],
            "text": lf["text"],
            "type": meta.get("type", "reference"),
            "status": meta.get("status", "current"),
            "related": meta.get("related", ""),
        })
    return out


def read_markdown(path: Path) -> str:
    return path.read_text(encoding="utf-8", errors="replace")


def walk_markdown(root: Path) -> list[Path]:
    return sorted(p for p in root.rglob("*") if p.suffix.lower() in (".md", ".markdown"))


def leaves_to_json(leaves: list[dict]) -> str:
    return json.dumps(leaves, ensure_ascii=False, indent=2)