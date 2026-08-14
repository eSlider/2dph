"""mailconv - pure helpers for bin/mail/import (mail -> markdown + attachments).

Shared with unit tests in bin/tools/test_mailconv.py. No network, no OnlyOffice
dependencies here: everything is `str -> str` or `Path -> str` so the tests run
offline against fixtures.
"""
from __future__ import annotations

import html
import os
import re
import subprocess
import tempfile
import zipfile
from pathlib import Path

# Body part / attachment file suffixes we know how to turn into markdown text.
TEXT_SUFFIXES = {".md", ".markdown", ".txt", ".csv", ".json", ".xml", ".yaml", ".yml", ".log", ".tsv",
                 ".ics", ".ical", ".vcf", ".eml"}
OFFICE_SUFFIXES = {".docx", ".pptx", ".xlsx", ".html", ".htm", ".epub", ".eml", ".msg"}
PDF_SUFFIXES = {".pdf"}
IMAGE_SUFFIXES = {".png", ".jpg", ".jpeg", ".gif", ".bmp", ".tiff", ".tif", ".webp"}
ARCHIVE_SUFFIXES = {".zip"}
# Legacy binary Office (doc/xls/ppt) — markitdown skip them; we try
# pandoc first, else leave a stub.
LEGACY_OFFICE_SUFFIXES = {".doc", ".xls", ".ppt"}
TESS_LANG = "eng+deu"

CONVERTIBLE_SUFFIXES = (
    TEXT_SUFFIXES | OFFICE_SUFFIXES | PDF_SUFFIXES | IMAGE_SUFFIXES | ARCHIVE_SUFFIXES | LEGACY_OFFICE_SUFFIXES
)


def clean_email_address(raw: str) -> str:
    """Extract the bare email from '"Name" <a@b.c>' and strip control chars."""
    m = re.search(r"<([^<>@\s]+@[^<>@\s]+)>", raw)
    return (m.group(1) if m else raw).strip()


def subject_to_filename(subject: str, max_len: int = 80) -> str:
    """Turn a mail subject into a filesystem-safe slug (keep first token readable)."""
    s = re.sub(r"[^\w\-. ]+", "", subject).strip()
    s = re.sub(r"\s+", "_", s)
    s = s.strip("._")
    if not s:
        s = "untitled"
    return s[:max_len] or "untitled"


def strip_html(html_text: str) -> str:
    """Naive HTML -> plain text fallback (used only if markitdown is missing)."""
    import re as _re
    text = _re.sub(r"(?is)<(script|style)[^>]*>.*?</\1>", "", html_text)
    text = _re.sub(r"(?s)<br\s*/?>", "\n", text)
    text = _re.sub(r"(?s)</p>", "\n\n", text)
    text = _re.sub(r"(?s)<[^>]+>", "", text)
    return html.unescape(text).strip()


def _unwrap_tables(html_text: str) -> str:
    """Unwrap mail HTML tables into pipe-joined text lines.

    Outlook/Stripe-style emails wrap content in nested spacer/frame tables that
    markitdown renders as hundreds of `--- |` cells and duplicated blocks.
    Every <table> becomes plain "cell1 | cell2" lines (key-value pairs survive),
    so only headings/paragraphs/links reach markitdown and no table noise is left.
    """
    try:
        from bs4 import BeautifulSoup
    except Exception:
        return html_text
    soup = BeautifulSoup(html_text, "html.parser")
    for table in reversed(soup.find_all("table")):
        lines: list[str] = []
        for row in table.find_all("tr"):
            cells = [c.get_text(" ", strip=True) for c in row.find_all(["td", "th"])]
            line = " | ".join(x for x in cells if x)
            if line:
                lines.append(line)
        if lines:
            table.replace_with(BeautifulSoup("\n".join(lines), "html.parser"))
        else:
            table.decompose()
    return str(soup)


def html_to_markdown(html_text: str) -> str:
    """Convert a mail HTML body to markdown using markitdown when available."""
    html_text = _unwrap_tables(html_text)
    try:
        from markitdown import MarkItDown
        import io
        md = MarkItDown()
        result = md.convert_stream(io.BytesIO(html_text.encode("utf-8", errors="replace")),
                                   file_extension=".html")
        text = result.text_content.strip()
        if text:
            return normalize_markdown(text)
    except Exception:
        pass
    return normalize_markdown(strip_html(html_text))


def normalize_markdown(text: str) -> str:
    """Collapse the pdfminer/markitdown NUL artifacts and stray control chars."""
    # NUL bytes that pdfminer inserts between digits/letters.
    text = text.replace("\x00", "")
    # Email spacer noise: zero-width chars, soft hyphens, figure spaces,
    # combining grapheme joiner, BOM.
    for ch in ("\ufeff", "\u200b", "\u034f", "\u00ad", "\u2007", "\u2008", "\u200a", "\u2002"):
        text = text.replace(ch, "")
    text = re.sub(r"[ \t]{2,}", " ", text)
    # Trim trailing whitespace per line so space-only spacer rows collapse.
    text = "\n".join(l.rstrip() for l in text.split("\n"))
    # Collapse 3+ blank lines to two.
    text = re.sub(r"\n{3,}", "\n\n", text)
    # Remove weird trailing control chars.
    text = "".join(ch for ch in text if ch >= " " or ch in "\n\t")
    return text.strip()


def split_zip_members(zip_path: Path) -> list[str]:
    """Return safe member names of a zip archive (skips dir entries)."""
    try:
        with zipfile.ZipFile(zip_path) as zf:
            return [m for m in zf.namelist() if not m.endswith("/")]
    except zipfile.BadZipFile:
        return []


def zip_extract_safe(zip_path: Path, dest: Path) -> list[Path]:
    """Extract a zip into dest guarding against path traversal; returns files."""
    out: list[Path] = []
    try:
        with zipfile.ZipFile(zip_path) as zf:
            for member in zf.infolist():
                if member.is_dir():
                    continue
                target = (dest / member.filename).resolve()
                if not target.is_relative_to(dest.resolve()):
                    continue
                target.parent.mkdir(parents=True, exist_ok=True)
                with zf.open(member) as src, open(target, "wb") as dst:
                    dst.write(src.read())
                out.append(target)
    except zipfile.BadZipFile:
        return []
    return out


def is_convertible(suffix: str) -> bool:
    return suffix.lower() in CONVERTIBLE_SUFFIXES


def convert_pdf(path: Path, ocr: bool = False) -> str:
    """pdftotext -layout first; empty text layer → pdftoppm + tesseract.

    `ocr` is unused for born-digital PDFs (text layer wins). Scans OCR
    automatically. This path never execs an ONNX document converter.
    """
    del ocr  # scans OCR when the text layer is empty; flag is for images
    text = pdf_fast_text(path)
    if text and text.strip():
        return normalize_markdown(text)
    scanned = ocr_pdf(path)
    if scanned and scanned.strip():
        return normalize_markdown(scanned)
    if text:
        return normalize_markdown(text)
    return "\n<!-- pdf has no text layer (ocr unavailable) -->\n"


def pdf_fast_text(path: Path) -> str | None:
    """pdftotext -layout; None when poppler is missing or the command fails."""
    try:
        proc = subprocess.run(
            ["pdftotext", "-layout", str(path), "-"],
            capture_output=True, timeout=60)
    except (OSError, subprocess.TimeoutExpired):
        return None
    if proc.returncode != 0:
        return None
    return proc.stdout.decode("utf-8", errors="replace")


def ocr_pdf(path: Path) -> str:
    """Rasterize with pdftoppm and OCR each page (tesseract or paddle)."""
    try:
        with tempfile.TemporaryDirectory(prefix="2dph-ocr-") as tmp:
            prefix = str(Path(tmp) / "page")
            proc = subprocess.run(
                ["pdftoppm", "-png", "-r", "200", str(path), prefix],
                capture_output=True, timeout=120)
            if proc.returncode != 0:
                return ""
            pages = sorted(Path(tmp).glob("page*.png"))
            parts = [ocr_image(p) for p in pages]
            return "\n\n".join(p for p in parts if p and p.strip())
    except (OSError, subprocess.TimeoutExpired):
        return ""


def ocr_image(path: Path) -> str:
    engine = os.environ.get("OCR_ENGINE", "tesseract")
    if engine == "paddle":
        return _ocr_paddle(path)
    return _ocr_tesseract(path)


def _ocr_tesseract(path: Path) -> str:
    try:
        proc = subprocess.run(
            ["tesseract", str(path), "stdout", "-l", TESS_LANG, "--psm", "6"],
            capture_output=True, timeout=120)
    except (OSError, subprocess.TimeoutExpired):
        return ""
    if proc.returncode != 0:
        return ""
    return proc.stdout.decode("utf-8", errors="replace").strip()


def _ocr_paddle(path: Path) -> str:
    try:
        proc = subprocess.run(
            ["paddleocr", "ocr", "-i", str(path)],
            capture_output=True, timeout=180)
    except (OSError, subprocess.TimeoutExpired):
        return ""
    if proc.returncode != 0:
        return ""
    return proc.stdout.decode("utf-8", errors="replace").strip()
