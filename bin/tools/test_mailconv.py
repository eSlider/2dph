import io
import os
import sys
import unittest
import unittest.mock
import zipfile
from pathlib import Path

sys.path.insert(0, os.path.dirname(__file__))

from mailconv import (  # noqa: E402
    TESS_LANG,
    clean_email_address,
    convert_pdf,
    html_to_markdown,
    is_convertible,
    normalize_markdown,
    ocr_image,
    split_zip_members,
    subject_to_filename,
    zip_extract_safe,
)
from mailconv import _unwrap_tables  # noqa: E402


class TestMailConv(unittest.TestCase):
    def test_clean_email_address(self):
        self.assertEqual(clean_email_address('"Ben Baker" <bb@teks.com>'), "bb@teks.com")
        self.assertEqual(clean_email_address("eslider@gmail.com"), "eslider@gmail.com")
        self.assertEqual(clean_email_address("<a@b.c>"), "a@b.c")

    def test_subject_to_filename(self):
        self.assertEqual(subject_to_filename("Your receipt #2422"), "Your_receipt_2422")
        self.assertEqual(subject_to_filename("a/b\\c:d*e"), "abcde")
        self.assertEqual(subject_to_filename("   "), "untitled")

    def test_html_to_markdown(self):
        out = html_to_markdown("<html><body><h1>Hi</h1><p>Some <b>bold</b> text.</p></body></html>")
        self.assertIn("Hi", out)
        self.assertIn("**bold**", out)

    def test_html_strip_fallback(self):
        from mailconv import strip_html
        self.assertEqual(strip_html("<p>a</p><p>b</p>"), "a\n\nb")

    def test_flatten_layout_tables(self):
        html = ("<table><tr>"
                + "".join(f"<td>spacer{i}</td>" for i in range(12))
                + "</tr></table>"
                + "<p>real</p>"
                + "<table><tr><td>a</td><td>b</td></tr></table>")
        out = _unwrap_tables(html)
        # tables unwrapped into pipe text; no <td> left; content preserved
        self.assertNotIn("<td>spacer0</td>", out)
        self.assertIn("spacer0 | spacer1", out)
        self.assertIn("a | b", out)
        self.assertIn("real", out)

    def test_html_to_markdown_layout_clean(self):
        html = "<table><tr>" + "".join(f"<td>x{i}</td>" for i in range(12)) + "</tr></table><h1>Hi</h1>"
        out = html_to_markdown(html)
        self.assertIn("Hi", out)
        self.assertNotIn("| ---", out)

    def test_normalize_markdown_removes_nul(self):
        self.assertEqual(normalize_markdown("Z0\x00A\x00Y\x00B"), "Z0AYB")
        self.assertEqual(normalize_markdown("a\n\n\n\nb"), "a\n\nb")

    def test_normalize_strips_email_noise(self):
        noisy = "\ufeffa\u200b\u034f\u00ad\u2007\u2002 b\u200a c\u2008"
        out = normalize_markdown(noisy)
        self.assertNotIn("\u200b", out)
        self.assertNotIn("\ufeff", out)
        self.assertNotIn("\u034f", out)
        self.assertIn("a b c", out)

    def test_split_zip_members(self):
        p = Path(self._mk_zip(["a.txt", "sub/b.txt"]))
        self.assertEqual(split_zip_members(p), ["a.txt", "sub/b.txt"])

    def test_zip_extract_safe(self):
        zip_path = self._mk_zip(["a.txt", "dir/b.txt"])
        dest = Path(self._tmp("x"))
        files = zip_extract_safe(zip_path, dest)
        self.assertEqual(len(files), 2)
        self.assertTrue((dest / "a.txt").exists())
        self.assertTrue((dest / "dir" / "b.txt").exists())

    def test_zip_extract_safe_blocks_traversal(self):
        # member "../evil.txt" must not escape dest
        zip_path = Path(self._tmp("evil.zip"))
        with zipfile.ZipFile(zip_path, "w") as zf:
            zf.writestr("../evil.txt", "boom")
        dest = Path(self._tmp("out"))
        files = zip_extract_safe(zip_path, dest)
        self.assertEqual(files, [])
        self.assertFalse((dest.parent / "evil.txt").exists())

    def test_zip_extract_safe_skips_encrypted_members(self):
        # An encrypted member raises RuntimeError on zf.open when no password
        # is supplied (zipfile raises this for flag-bit-0 members). It must be
        # skipped, not abort the whole archive (and never the whole import).
        zip_path = Path(self._tmp("enc.zip"))
        with zipfile.ZipFile(zip_path, "w") as zf:
            zf.writestr("ok.txt", "clear")
            zf.writestr("secret.pdf", b"\x50\x4bencrypted")
        dest = Path(self._tmp("out"))
        real_open = zipfile.ZipFile.open

        def boom(self_, name, *a, **kw):
            member = getattr(name, "filename", name)
            if member == "secret.pdf":
                raise RuntimeError("File %r is encrypted, password required" % member)
            return real_open(self_, name, *a, **kw)

        with unittest.mock.patch.object(zipfile.ZipFile, "open", boom):
            files = zip_extract_safe(zip_path, dest)
        self.assertGreaterEqual(len(files), 1)
        self.assertTrue((dest / "ok.txt").exists())
        self.assertFalse((dest / "secret.pdf").exists())

    def test_is_convertible(self):
        self.assertTrue(is_convertible(".pdf"))
        self.assertTrue(is_convertible(".zip"))
        self.assertTrue(is_convertible(".docx"))
        self.assertTrue(is_convertible(".TXT"))
        self.assertFalse(is_convertible(".exe"))
        self.assertFalse(is_convertible(".unknown"))

    def test_convert_pdf_prefers_pdftotext(self):
        import mailconv as mc

        calls: list[list[str]] = []

        def fake_run(cmd, **kwargs):
            calls.append(list(cmd))

            class P:
                returncode = 0
                stdout = b"Invoice BM25 layout"
                stderr = b""

            return P()

        self._patch_run(mc, fake_run)
        out = convert_pdf(Path(self._tmp("born.pdf")))
        self.assertIn("BM25", out)
        self.assertEqual(calls[0][:2], ["pdftotext", "-layout"])
        self.assertFalse(any(c[0] == "tesseract" for c in calls))
        self.assertFalse(any(c[0] == "pdftoppm" for c in calls))

    def test_convert_pdf_empty_layer_uses_pdftoppm_tesseract(self):
        import mailconv as mc

        calls: list[list[str]] = []

        def fake_run(cmd, **kwargs):
            calls.append(list(cmd))

            class P:
                returncode = 0
                stdout = b""
                stderr = b""

            if cmd[0] == "pdftotext":
                P.stdout = b"  \n"
                return P()
            if cmd[0] == "pdftoppm":
                prefix = Path(cmd[-1])
                (prefix.parent / "page-1.png").write_bytes(b"fake")
                return P()
            if cmd[0] == "tesseract":
                P.stdout = b"scanned HELLO"
                return P()
            return P()

        self._patch_run(mc, fake_run)
        out = convert_pdf(Path(self._tmp("scan.pdf")))
        self.assertIn("HELLO", out)
        bins = [c[0] for c in calls]
        self.assertIn("pdftotext", bins)
        self.assertIn("pdftoppm", bins)
        self.assertIn("tesseract", bins)
        tess = next(c for c in calls if c[0] == "tesseract")
        self.assertIn(TESS_LANG, tess)
        self.assertNotIn("docling", " ".join(bins))

    def test_ocr_image_paddle_engine(self):
        import mailconv as mc

        calls: list[list[str]] = []

        def fake_run(cmd, **kwargs):
            calls.append(list(cmd))

            class P:
                returncode = 0
                stdout = b"paddle text"
                stderr = b""

            return P()

        self._patch_run(mc, fake_run)
        os.environ["OCR_ENGINE"] = "paddle"
        try:
            out = ocr_image(Path(self._tmp("x.png")))
        finally:
            os.environ.pop("OCR_ENGINE", None)
        self.assertEqual(out, "paddle text")
        self.assertEqual(calls[0][:2], ["paddleocr", "ocr"])

    def _patch_run(self, mod, fn) -> None:
        self.addCleanup(setattr, mod.subprocess, "run", mod.subprocess.run)
        mod.subprocess.run = fn

    def _mk_zip(self, members):
        zpath = Path(self._tmp("arc.zip"))
        with zipfile.ZipFile(zpath, "w") as zf:
            for m in members:
                zf.writestr(m, "content")
        return str(zpath)

    def _tmp(self, name):
        d = self.__class__._td
        p = Path(d) / name
        p.parent.mkdir(parents=True, exist_ok=True)
        return str(p)

    @classmethod
    def setUpClass(cls):
        import tempfile
        cls._td = tempfile.mkdtemp(prefix="mailconv_test_")


if __name__ == "__main__":
    unittest.main()
