#!/bin/sh
# Fixture search binary for ExecSearcher tests (issue #202): ignores the
# query, prints a fixed search JSON payload — the same shape as
# bin/brain/search.go --json output. IDs match the bench test baseline truth.
cat <<'EOF'
{"query":"fixture","count":2,"results":[{"id":"1","text":"BM25 ranks best-first","root":"info"},{"id":"b2","text":"tesseract ocr pdf","root":"facts"}]}
EOF
