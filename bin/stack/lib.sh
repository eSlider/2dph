# bin/stack/lib.sh — compose helpers for start / start-assistant / stop / status.
# Sourced, not executed. No secrets. No host-absolute paths.

BRAIN_URL="${BRAIN_URL:-http://127.0.0.1:8630}"
REASONER_URL="${REASONER_URL:-http://127.0.0.1:11435}"
PICOCLAW_URL="${PICOCLAW_URL:-http://127.0.0.1:18790}"
REASONER_MODEL="${REASONER_MODEL:-qwen3.5:9b}"
STACK_WAIT_SECS="${STACK_WAIT_SECS:-90}"
STACK_WAIT_INTERVAL="${STACK_WAIT_INTERVAL:-2}"
STACK_PULL_SECS="${STACK_PULL_SECS:-600}"

if [[ -z "${ROOT:-}" ]]; then
	STACK_DIR="$(CDPATH= cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
	ROOT="$(CDPATH= cd -- "$STACK_DIR/../.." && pwd)"
fi

stack_usage() {
	awk 'NR == 1 { next } /^#/ { sub(/^# ?/, ""); print; next } { exit }' "$1"
}

stack_die() {
	echo "bin/stack: $*" >&2
	return 1
}

compose() {
	docker compose -f "$ROOT/compose.yaml" --project-directory "$ROOT" "$@"
}

http_get() {
	local url=$1
	local timeout=${2:-5}
	curl -sS --max-time "$timeout" "$url" 2>/dev/null || return 1
}

health_ok() {
	local url=$1
	local timeout=${2:-5}
	local body
	body=$(http_get "$url" "$timeout") || return 1
	printf '%s' "$body" | grep -q '"status":"ok"'
}

wait_health() {
	local url=$1
	local n=0
	while ((n <= STACK_WAIT_SECS)); do
		if health_ok "$url"; then
			return 0
		fi
		n=$((n + 1))
		if ((n <= STACK_WAIT_SECS)); then
			sleep "$STACK_WAIT_INTERVAL"
		fi
	done
	return 1
}

wait_http() {
	local url=$1
	local n=0
	while ((n <= STACK_WAIT_SECS)); do
		if http_get "$url" 5 >/dev/null; then
			return 0
		fi
		n=$((n + 1))
		if ((n <= STACK_WAIT_SECS)); then
			sleep "$STACK_WAIT_INTERVAL"
		fi
	done
	return 1
}

mcp_body() {
	curl -sS --max-time 10 \
		-H 'Content-Type: application/json' \
		-d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' \
		"$BRAIN_URL/mcp" 2>/dev/null || return 1
}

mcp_ok() {
	local body
	body=$(mcp_body) || return 1
	printf '%s' "$body" | grep -Eq '"name": ?"search"' || return 1
	printf '%s' "$body" | grep -Eq '"name": ?"get"' || return 1
	printf '%s' "$body" | grep -Eq '"name": ?"audit"' || return 1
	return 0
}

reasoner_tags() {
	http_get "$REASONER_URL/api/tags" 5
}

reasoner_has_model() {
	local body
	body=$(reasoner_tags) || return 1
	printf '%s' "$body" | grep -Fq "$REASONER_MODEL"
}

ensure_mcp() {
	mcp_ok || stack_die "MCP tools/list missing search/get/audit at $BRAIN_URL/mcp"
}

ensure_brain() {
	if health_ok "$BRAIN_URL/health"; then
		echo "brain: reuse $BRAIN_URL" >&2
	else
		echo "brain: compose up" >&2
		compose up -d brain
		wait_health "$BRAIN_URL/health" || stack_die "brain health failed at $BRAIN_URL/health"
	fi
	ensure_mcp
}

pull_reasoner_model() {
	echo "reasoner: pulling $REASONER_MODEL (CPU, may take minutes)" >&2
	curl -sS --max-time "$STACK_PULL_SECS" \
		-H 'Content-Type: application/json' \
		-d "{\"name\":\"$REASONER_MODEL\"}" \
		"$REASONER_URL/api/pull" >/dev/null
}

ensure_reasoner() {
	if reasoner_has_model; then
		echo "reasoner: reuse $REASONER_URL model $REASONER_MODEL" >&2
		return 0
	fi
	if ! reasoner_tags >/dev/null; then
		echo "reasoner: compose up" >&2
		compose --profile reasoner up -d reasoner
		wait_http "$REASONER_URL/api/tags" || stack_die "reasoner not listening at $REASONER_URL"
	fi
	if reasoner_has_model; then
		return 0
	fi
	pull_reasoner_model
	reasoner_has_model || stack_die "reasoner missing model $REASONER_MODEL"
}

ensure_picoclaw() {
	echo "picoclaw: compose up --no-deps (reuse healthy :8630/:11435)" >&2
	compose --profile picoclaw up -d --no-deps picoclaw
	wait_health "$PICOCLAW_URL/health" || stack_die "picoclaw health failed at $PICOCLAW_URL/health"
}

mail_sync_running() {
	compose ps --status running --services 2>/dev/null | grep -qx mail-sync
}

stack_status() {
	local bh=down mcp=down ph=down present=false ms=down
	health_ok "$BRAIN_URL/health" && bh=ok
	mcp_ok && mcp=ok
	reasoner_has_model && present=true
	health_ok "$PICOCLAW_URL/health" && ph=ok
	mail_sync_running && ms=ok
	cat <<EOF
brain:
  url: $BRAIN_URL
  health: $bh
  mcp: $mcp
reasoner:
  url: $REASONER_URL
  model: $REASONER_MODEL
  present: $present
picoclaw:
  url: $PICOCLAW_URL
  health: $ph
mail_sync:
  service: mail-sync
  running: $ms
EOF
}

stack_start() {
	ensure_brain
}

stack_start_mail_sync() {
	echo "mail-sync: compose up (ETL sync→import; index only if MAIL_SYNC_INDEX=1)" >&2
	compose up -d mail-sync
}

stack_attach_agent() {
	local opts=()
	if [[ -t 0 && -t 1 ]]; then
		opts+=(-it)
	else
		opts+=(-T)
	fi
	if [[ (! -t 0 || ! -t 1) && $# -eq 0 ]]; then
		echo "picoclaw: no TTY. Attach with:" >&2
		echo "  $ROOT/bin/stack/start-assistant" >&2
		echo "  docker compose --profile picoclaw exec -it picoclaw picoclaw agent" >&2
		return 0
	fi
	echo "picoclaw: agent (search → get → audit before a factual reply)" >&2
	exec docker compose -f "$ROOT/compose.yaml" --project-directory "$ROOT" \
		--profile picoclaw exec "${opts[@]}" picoclaw picoclaw agent "$@"
}

stack_start_assistant() {
	local attach=1
	local agent_args=()
	while (($#)); do
		case "$1" in
		-h | --help)
			stack_usage "$ROOT/bin/stack/start-assistant"
			return 0
			;;
		--no-attach)
			attach=0
			shift
			;;
		--)
			shift
			agent_args+=("$@")
			break
			;;
		*)
			agent_args+=("$1")
			shift
			;;
		esac
	done
	stack_start
	ensure_reasoner
	ensure_picoclaw
	stack_status
	if ((attach == 0)); then
		echo "picoclaw: gateway $PICOCLAW_URL (agent not attached)" >&2
		echo "ask the brain: $ROOT/bin/stack/start-assistant" >&2
		echo "one-shot: $ROOT/bin/stack/start-assistant -- -m \"search the 2dph brain for LadybugDB\"" >&2
		return 0
	fi
	stack_attach_agent "${agent_args[@]}"
}

stack_stop() {
	case "${1:-}" in
	-h | --help)
		stack_usage "$ROOT/bin/stack/stop"
		return 0
		;;
	esac
	echo "stack: stop brain brain-mcp reasoner picoclaw mail-sync (volumes kept)" >&2
	compose --profile picoclaw --profile reasoner stop picoclaw brain-mcp reasoner brain mail-sync
}
