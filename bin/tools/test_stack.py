"""bin/stack/{start,start-assistant,stop,status} — offline contract + fake PATH."""
from __future__ import annotations

import os
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
METHODS = ("start", "start-assistant", "stop", "status")


class StackLayoutTest(unittest.TestCase):
    def test_methods_are_bash_with_usage_comment(self) -> None:
        lib = ROOT / "bin" / "stack" / "lib.sh"
        self.assertTrue(lib.is_file(), "missing bin/stack/lib.sh")
        for name in METHODS:
            p = ROOT / "bin" / "stack" / name
            self.assertTrue(p.is_file(), f"missing bin/stack/{name}")
            self.assertTrue(os.access(p, os.X_OK), f"bin/stack/{name} must be executable")
            lines = p.read_text().splitlines()
            self.assertEqual(lines[0], "#!/usr/bin/env bash", name)
            self.assertTrue(lines[1].startswith("# bin/stack/"), name)
            text = "\n".join(lines)
            self.assertIn("lib.sh", text, name)
            self.assertNotIn("/mnt/", text, name)
            self.assertNotIn("/home/", text, name)

    def test_lib_has_no_host_paths_or_secrets(self) -> None:
        lib = (ROOT / "bin" / "stack" / "lib.sh").read_text()
        self.assertIn("stack_start", lib)
        self.assertIn("stack_start_assistant", lib)
        self.assertIn("stack_stop", lib)
        self.assertIn("stack_status", lib)
        self.assertIn("qwen3.5:9b", lib)
        self.assertIn("picoclaw agent", lib)
        self.assertIn("--no-deps", lib)
        self.assertIn("tools/list", lib)
        self.assertNotIn("/mnt/", lib)
        self.assertNotIn("/home/", lib)
        self.assertNotIn("password", lib.lower())
        self.assertNotIn("GITEA_TOKEN", lib)

    def test_start_does_not_launch_picoclaw(self) -> None:
        start = (ROOT / "bin" / "stack" / "start").read_text()
        self.assertIn("stack_start", start)
        self.assertNotIn("stack_start_assistant", start)
        self.assertNotIn("picoclaw agent", start)

    def test_start_assistant_attaches_agent(self) -> None:
        src = (ROOT / "bin" / "stack" / "start-assistant").read_text()
        self.assertIn("stack_start_assistant", src)
        self.assertIn("--no-attach", src)

    def test_stop_does_not_down_volumes(self) -> None:
        lib = (ROOT / "bin" / "stack" / "lib.sh").read_text()
        self.assertIn(" compose ", lib)
        self.assertRegex(lib, r"\bstop\b")
        self.assertNotIn(" compose down", lib)
        self.assertNotIn("compose down", lib)

    def test_docs_name_stack_commands(self) -> None:
        runbook = (ROOT / "docs" / "runbook.md").read_text()
        pico = (ROOT / "docs" / "picoclaw.md").read_text()
        agents = (ROOT / "AGENTS.md").read_text()
        for text in (runbook, pico, agents):
            self.assertIn("bin/stack/start", text)
            self.assertIn("bin/stack/start-assistant", text)
            self.assertIn("bin/stack/status", text)
            self.assertIn("bin/stack/stop", text)


    def test_help_prints_comments_not_source(self) -> None:
        r = subprocess.run(
            [str(ROOT / "bin" / "stack" / "start"), "--help"],
            cwd=str(ROOT),
            capture_output=True,
            text=True,
            check=False,
        )
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("bin/stack/start", r.stdout)
        self.assertNotIn("set -euo pipefail", r.stdout)
        self.assertNotIn("source ", r.stdout)


class StackFakePathTest(unittest.TestCase):
    def _fake_bin(self, tmp: Path, *, health_ok: bool) -> Path:
        bindir = tmp / "bin"
        bindir.mkdir()
        curl = bindir / "curl"
        docker = bindir / "docker"
        log = tmp / "docker.log"
        curl.write_text(
            f"""#!/usr/bin/env bash
url=""
for a in "$@"; do
  case "$a" in http*) url=$a ;;
  esac
done
if [ "{int(health_ok)}" = "0" ] && [[ "$url" == */health ]]; then
  echo '{{"status":"down"}}'
  exit 7
fi
case "$url" in
  */mcp)
    echo '{{"jsonrpc":"2.0","id":1,"result":{{"tools":[{{"name":"search"}},{{"name":"get"}},{{"name":"audit"}}]}}}}'
    ;;
  */api/tags)
    echo '{{"models":[{{"name":"qwen3.5:9b"}}]}}'
    ;;
  */api/pull)
    echo '{{"status":"success"}}'
    ;;
  *)
    echo '{{"status":"ok"}}'
    ;;
esac
"""
        )
        docker.write_text(
            f"""#!/usr/bin/env bash
echo "$*" >> "{log}"
exit 0
"""
        )
        curl.chmod(curl.stat().st_mode | stat.S_IEXEC)
        docker.chmod(docker.stat().st_mode | stat.S_IEXEC)
        return bindir

    def _env(self, bindir: Path) -> dict[str, str]:
        env = os.environ.copy()
        env["PATH"] = f"{bindir}:{env.get('PATH', '')}"
        env["STACK_WAIT_SECS"] = "1"
        env["STACK_WAIT_INTERVAL"] = "0"
        return env

    def test_start_skips_compose_when_brain_healthy(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            tmp = Path(raw)
            bindir = self._fake_bin(tmp, health_ok=True)
            log = tmp / "docker.log"
            r = subprocess.run(
                [str(ROOT / "bin" / "stack" / "start")],
                cwd=str(ROOT),
                env=self._env(bindir),
                capture_output=True,
                text=True,
                check=False,
            )
            self.assertEqual(r.returncode, 0, r.stderr)
            self.assertFalse(log.exists(), "healthy brain must not docker compose up")

    def test_start_ups_brain_when_unhealthy(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            tmp = Path(raw)
            bindir = self._fake_bin(tmp, health_ok=False)
            log = tmp / "docker.log"
            r = subprocess.run(
                [str(ROOT / "bin" / "stack" / "start")],
                cwd=str(ROOT),
                env=self._env(bindir),
                capture_output=True,
                text=True,
                check=False,
            )
            self.assertNotEqual(r.returncode, 0, "unhealthy brain without recovering compose must fail")
            self.assertTrue(log.exists(), r.stderr)
            logged = log.read_text()
            self.assertIn("up -d", logged)
            self.assertIn("brain", logged)
            self.assertNotIn("picoclaw", logged)

    def test_stop_stops_named_services(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            tmp = Path(raw)
            bindir = self._fake_bin(tmp, health_ok=True)
            log = tmp / "docker.log"
            r = subprocess.run(
                [str(ROOT / "bin" / "stack" / "stop")],
                cwd=str(ROOT),
                env=self._env(bindir),
                capture_output=True,
                text=True,
                check=False,
            )
            self.assertEqual(r.returncode, 0, r.stderr)
            logged = log.read_text()
            self.assertIn("stop", logged)
            self.assertNotIn(" down", logged)
            for svc in ("brain", "brain-mcp", "reasoner", "picoclaw"):
                self.assertIn(svc, logged)

    def test_start_assistant_no_attach_starts_picoclaw(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            tmp = Path(raw)
            bindir = self._fake_bin(tmp, health_ok=True)
            log = tmp / "docker.log"
            r = subprocess.run(
                [str(ROOT / "bin" / "stack" / "start-assistant"), "--no-attach"],
                cwd=str(ROOT),
                env=self._env(bindir),
                capture_output=True,
                text=True,
                check=False,
            )
            self.assertEqual(r.returncode, 0, r.stderr + r.stdout)
            logged = log.read_text() if log.exists() else ""
            self.assertIn("picoclaw", logged)
            self.assertIn("--no-deps", logged)
            self.assertNotIn("picoclaw agent", logged)
