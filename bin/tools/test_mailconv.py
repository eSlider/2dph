import io
import os
import sys
import unittest
import zipfile
from pathlib import Path

sys.path.insert(0, os.path.dirname(__file__))

from mailconv import (  # noqa: E402
    clean_email_address,
    html_to_markdown,
    is_convertible,
    normalize_markdown,
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

    def test_is_convertible(self):
        self.assertTrue(is_convertible(".pdf"))
        self.assertTrue(is_convertible(".zip"))
        self.assertTrue(is_convertible(".docx"))
        self.assertTrue(is_convertible(".TXT"))
        self.assertFalse(is_convertible(".exe"))
        self.assertFalse(is_convertible(".unknown"))

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
