#!/usr/bin/env python3
"""System performance test: PicoClaw surface (brain MCP) + optional reasoner.

  BRAIN_URL=http://127.0.0.1:8630 ./qa/system_perf.py --json
  REASONER_BASE_URL=http://127.0.0.1:11435/v1 REASONER_MODEL=qwen3.5:9b \\
    ./qa/system_perf.py --reasoner --picoclaw --json

Does not write Ladybug. Search includes web (D17); expect ~10s+ per search.
Exit 1 if health/get/audit gates fail. Reasoner is measured, not gated.
"""
from __future__ import annotations

import argparse
import json
import os
import statistics
import sys
import time
import urllib.error
import urllib.request
from concurrent.futures import ThreadPoolExecutor

DEFAULT_BRAIN = "http://127.0.0.1:8630"
DEFAULT_REASONER = "http://127.0.0.1:11435/v1"
DEFAULT_MODEL = "qwen3.5:9b"
DEFAULT_PICOCLAW = "http://127.0.0.1:18790"

GATE_HEALTH_MS = 500
GATE_GET_P50_MS = 50
GATE_AUDIT_P50_MS = 50


def _req(url: str, data: bytes | None = None, timeout: float = 90) -> bytes:
    headers = {"Content-Type": "application/json"} if data is not None else {}
    req = urllib.request.Request(url, data=data, headers=headers)
    with urllib.request.urlopen(req, timeout=timeout) as res:
        return res.read()


def timed(fn):
    t0 = time.perf_counter()
    out = fn()
    return (time.perf_counter() - t0) * 1000.0, out


def stats(samples: list[float]) -> dict:
    s = sorted(samples)
    n = len(s)
    return {
        "n": n,
        "min_ms": round(s[0], 1),
        "p50_ms": round(s[n // 2], 1),
        "p95_ms": round(s[min(n - 1, int(n * 0.95))], 1),
        "max_ms": round(s[-1], 1),
        "avg_ms": round(statistics.mean(s), 1),
    }


def mcp(brain: str, method: str, params=None, timeout: float = 90) -> dict:
    payload: dict = {"jsonrpc": "2.0", "id": 1, "method": method}
    if params is not None:
        payload["params"] = params
    raw = _req(brain.rstrip("/") + "/mcp", json.dumps(payload).encode(), timeout=timeout)
    return json.loads(raw.decode())


def mcp_call(brain: str, name: str, arguments: dict, timeout: float = 90) -> tuple[bool, str]:
    d = mcp(brain, "tools/call", {"name": name, "arguments": arguments}, timeout=timeout)
    res = d.get("result") or {}
    text = ((res.get("content") or [{}])[0].get("text") or "")
    return (not res.get("isError")), text


def reasoner_tool_call(base: str, model: str, user: str) -> str:
    payload = {
        "model": model,
        "messages": [
            {"role": "system", "content": "You are PicoClaw. Always call search before answering."},
            {"role": "user", "content": user},
        ],
        "tools": [
            {
                "type": "function",
                "function": {
                    "name": "search",
                    "description": "deduction search",
                    "parameters": {
                        "type": "object",
                        "properties": {"q": {"type": "string"}},
                        "required": ["q"],
                    },
                },
            }
        ],
        "tool_choice": "required",
    }
    raw = _req(
        base.rstrip("/") + "/chat/completions",
        json.dumps(payload).encode(),
        timeout=600,
    )
    chat = json.loads(raw.decode())
    tcs = chat["choices"][0]["message"].get("tool_calls") or []
    if not tcs:
        return ""
    return tcs[0]["function"]["name"]


def run(args: argparse.Namespace) -> dict:
    brain = args.brain.rstrip("/")
    report: dict = {
        "brain": brain,
        "device": "cpu",
        "ok": True,
        "gates": {},
        "mcp": {},
    }
    ms, _ = timed(lambda: _req(brain + "/health", timeout=5))
    report["mcp"]["health"] = {"n": 1, "avg_ms": round(ms, 1)}
    report["gates"]["health"] = ms <= GATE_HEALTH_MS
    if ms > GATE_HEALTH_MS:
        report["ok"] = False

    list_ms = []
    for _ in range(args.n):
        ms, d = timed(lambda: mcp(brain, "tools/list", timeout=10))
        names = [t["name"] for t in ((d.get("result") or {}).get("tools") or [])]
        if "search" not in names:
            report["ok"] = False
        list_ms.append(ms)
    report["mcp"]["tools_list"] = stats(list_ms)

    audit_ms = []
    for _ in range(args.n):
        ms, (ok, _) = timed(lambda: mcp_call(brain, "audit", {}))
        if not ok:
            report["ok"] = False
        audit_ms.append(ms)
    report["mcp"]["audit"] = stats(audit_ms)
    report["gates"]["audit_p50"] = report["mcp"]["audit"]["p50_ms"] <= GATE_AUDIT_P50_MS
    if not report["gates"]["audit_p50"]:
        report["ok"] = False

    ok, text = mcp_call(brain, "search", {"q": "LadybugDB", "n": 2}, timeout=90)
    inner = json.loads(text) if ok else {}
    hits = inner.get("results") or []
    leaf_id = hits[0]["id"] if hits else ""
    report["mcp"]["search_seed"] = {
        "ok": ok,
        "count": inner.get("count"),
        "web": (inner.get("web") or {}).get("status"),
    }

    get_ms = []
    if leaf_id:
        for _ in range(args.n):
            ms, (ok, _) = timed(lambda: mcp_call(brain, "get", {"id": leaf_id, "body": True}))
            if not ok:
                report["ok"] = False
            get_ms.append(ms)
        report["mcp"]["get"] = stats(get_ms)
        report["gates"]["get_p50"] = report["mcp"]["get"]["p50_ms"] <= GATE_GET_P50_MS
        if not report["gates"]["get_p50"]:
            report["ok"] = False

        def one_get() -> float:
            t0 = time.perf_counter()
            mcp_call(brain, "get", {"id": leaf_id, "body": True})
            return (time.perf_counter() - t0) * 1000.0

        t0 = time.perf_counter()
        with ThreadPoolExecutor(max_workers=8) as ex:
            conc = list(ex.map(lambda _: one_get(), range(8)))
        wall = (time.perf_counter() - t0) * 1000.0
        report["mcp"]["get_concurrent_8"] = {**stats(conc), "wall_ms": round(wall, 1)}

    search_ms = []
    for q in ("LadybugDB", "model2vec"):
        ms, (ok, text) = timed(lambda q=q: mcp_call(brain, "search", {"q": q, "n": 3}, timeout=90))
        inner = json.loads(text) if ok else {}
        search_ms.append(ms)
        report.setdefault("mcp", {}).setdefault("search_samples", []).append(
            {
                "q": q,
                "ms": round(ms, 1),
                "ok": ok,
                "count": inner.get("count"),
                "web": (inner.get("web") or {}).get("status"),
            }
        )
    if search_ms:
        report["mcp"]["search"] = stats(search_ms)

    if args.reasoner:
        base = args.reasoner_url
        model = args.model
        report["reasoner"] = {"base_url": base, "model": model, "calls": []}
        for user in (
            "Use tools. Search the 2dph brain for LadybugDB. Call search.",
            "Use tools. Search the 2dph brain for model2vec. Call search.",
        ):
            ms, name = timed(lambda user=user: reasoner_tool_call(base, model, user))
            report["reasoner"]["calls"].append({"ms": round(ms, 1), "tool": name})
        tools = [c["tool"] for c in report["reasoner"]["calls"]]
        report["gates"]["reasoner_tool_call"] = bool(tools) and all(t == "search" for t in tools)
        if not report["gates"]["reasoner_tool_call"]:
            report["ok"] = False

    if args.picoclaw:
        gw = args.picoclaw_url.rstrip("/")
        ms, raw = timed(lambda: _req(gw + "/health", timeout=5))
        body = json.loads(raw.decode())
        report["picoclaw"] = {
            "url": gw,
            "health_ms": round(ms, 1),
            "status": body.get("status"),
        }
        report["gates"]["picoclaw_health"] = body.get("status") == "ok" and ms <= GATE_HEALTH_MS
        if not report["gates"]["picoclaw_health"]:
            report["ok"] = False
    return report


def main(argv: list[str]) -> int:
    p = argparse.ArgumentParser(description="2dph system performance (MCP + optional reasoner)")
    p.add_argument("--brain", default=os.environ.get("BRAIN_URL", DEFAULT_BRAIN))
    p.add_argument("--n", type=int, default=20)
    p.add_argument("--json", action="store_true")
    p.add_argument("--reasoner", action="store_true")
    p.add_argument("--picoclaw", action="store_true")
    p.add_argument("--picoclaw-url", default=os.environ.get("PICOCLAW_URL", DEFAULT_PICOCLAW))
    p.add_argument("--reasoner-url", default=os.environ.get("REASONER_BASE_URL", DEFAULT_REASONER))
    p.add_argument("--model", default=os.environ.get("REASONER_MODEL", DEFAULT_MODEL))
    args = p.parse_args(argv)
    try:
        report = run(args)
    except (urllib.error.URLError, TimeoutError, OSError) as e:
        print(f"system_perf: {e}", file=sys.stderr)
        return 1
    if args.json:
        print(json.dumps(report, indent=2))
    else:
        print(f"ok={report['ok']} brain={report['brain']}")
        for name, block in report.get("mcp", {}).items():
            if isinstance(block, dict) and "p50_ms" in block:
                print(f"  {name}: p50={block['p50_ms']} p95={block['p95_ms']} n={block['n']}")
            elif name == "health":
                print(f"  health: {block.get('avg_ms')} ms")
        for k, v in report.get("gates", {}).items():
            print(f"  gate {k}: {v}")
        for c in (report.get("reasoner") or {}).get("calls") or []:
            print(f"  reasoner {c['tool']}: {c['ms']} ms")
    return 0 if report["ok"] else 1


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
