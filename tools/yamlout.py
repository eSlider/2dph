"""Minimal YAML emitter.

Agents read YAML more cheaply than JSON and the output stays diffable. This is
deliberately tiny: it emits the shapes these tools produce, nothing more.
"""
from __future__ import annotations

import json


def to_yaml(node, indent: int = 0) -> str:
    pad = "  " * indent
    if isinstance(node, dict):
        if not node:
            return f"{pad}{{}}\n"
        out = ""
        for key, value in node.items():
            if isinstance(value, (dict, list)) and value:
                out += f"{pad}{key}:\n{to_yaml(value, indent + 1)}"
            elif isinstance(value, (dict, list)):
                out += f"{pad}{key}: {'{}' if isinstance(value, dict) else '[]'}\n"
            else:
                out += f"{pad}{key}: {scalar(value)}\n"
        return out
    if isinstance(node, list):
        out = ""
        for item in node:
            if isinstance(item, dict):
                out += f"{pad}-\n{to_yaml(item, indent + 1)}"
            else:
                out += f"{pad}- {scalar(item)}\n"
        return out
    return f"{pad}{scalar(node)}\n"


def scalar(value) -> str:
    if value is None:
        return "null"
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, (int, float)):
        return str(value)
    text = str(value)
    if "\n" in text:
        return json.dumps(text, ensure_ascii=False)
    if text == "" or any(ch in text for ch in ":#'\"[]{}&*!|>%@`") or text != text.strip():
        return json.dumps(text, ensure_ascii=False)
    return text
