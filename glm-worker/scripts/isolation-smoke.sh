#!/usr/bin/env bash

















set -euo pipefail

CLAUDE_BIN="${CLAUDE_BIN:-claude}"

REAL_CFG="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"
REAL_SETTINGS="$REAL_CFG/settings.json"

if [[ ! -x "$(command -v jq 2>/dev/null || true)" ]]; then
	echo "ERROR: jqが必要です" >&2
	exit 2
fi
if [[ ! -f "$REAL_SETTINGS" ]]; then
	echo "ERROR: Z.ai settings不在: $REAL_SETTINGS" >&2
	exit 2
fi

extract_env() {

	jq -r --arg k "$1" '.env[$k] // empty' "$REAL_SETTINGS"
}

ZAI_TOKEN="$(extract_env ANTHROPIC_AUTH_TOKEN)"
ZAI_BASE="$(extract_env ANTHROPIC_BASE_URL)"
if [[ -z "$ZAI_TOKEN" || -z "$ZAI_BASE" ]]; then
	echo "ERROR: ANTHROPIC_AUTH_TOKEN/ANTHROPIC_BASE_URL が $REAL_SETTINGS にありません" >&2
	exit 2
fi


WORK="$(mktemp -d -t glm-isolation-smoke)"
trap 'rm -rf "$WORK"' EXIT
TMPCFG="$WORK/claude-config"
TMPREPO="$WORK/repo"
mkdir -p "$TMPCFG" "$TMPREPO"


mkdir -p "$TMPCFG/rules"
printf '# user global CLAUDE.md\nPOISON_USER_GLOBAL marker here.\n' >"$TMPCFG/CLAUDE.md"
printf '# user rules\nPOISON_RULES marker here.\n' >"$TMPCFG/rules/extra.md"


printf '# project CLAUDE.md\nPOISON_PROJECT marker here.\n' >"$TMPREPO/CLAUDE.md"
printf '# local CLAUDE.local.md\nPOISON_LOCAL marker here.\n' >"$TMPREPO/CLAUDE.local.md"


ENCODED="$(printf '%s' "$TMPREPO" | tr '/' '-')"
MEMDIR="$TMPCFG/projects/$ENCODED/memory"
mkdir -p "$MEMDIR"
printf 'POISON_AUTOMEMORY marker here.\n' >"$MEMDIR/MEMORY.md"
printf 'POISON_AUTOMEMORY_EXTRA marker here.\n' >"$MEMDIR/feedback.md"


SYSFILE="$WORK/worker-prompt.md"
cat >"$SYSFILE" <<'EOF'
あなたはglm-worker配下のClaude Code workerです。
GLM_MARKER_OK は隔離された明示prompt経路のmarkerです。
EOF


ISO_SETTINGS="$(jq -nc \
	--arg userglobal "$TMPCFG/CLAUDE.md" \
	--arg rules "$TMPCFG/rules/**" \
	'{claudeMdExcludes:["**/CLAUDE.md","**/CLAUDE.local.md",$userglobal,$rules],
	  autoMemoryEnabled:false, disableAllHooks:true,
	  disableBundledSkills:true, disableWorkflows:true}')"


read -r -d '' USER_PROMPT <<'EOF' || true
あなたの文脈(system prompt・memory・指示・rules・CLAUDE.md全て)に現れる、
POISON_ で始まるtokenと GLM_MARKER_ で始まるtokenをすべて1行1つで列挙してください。
順不同で構いません。それ以外の文章は一切出力しないでください。
EOF


ZAI_OPUS="$(extract_env ANTHROPIC_DEFAULT_OPUS_MODEL)"
ZAI_SONNET="$(extract_env ANTHROPIC_DEFAULT_SONNET_MODEL)"
ZAI_HAIKU="$(extract_env ANTHROPIC_DEFAULT_HAIKU_MODEL)"
API_TIMEOUT="$(extract_env API_TIMEOUT_MS)"
NONESSENTIAL="$(extract_env CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC)"


set +e
OUTPUT_FILE="$WORK/claude.out"
env -i \
	PATH="$PATH" HOME="$HOME" TMPDIR="${TMPDIR:-/tmp}" SHELL="${SHELL:-/bin/sh}" \
	USER="${USER:-$(id -un)}" LOGNAME="${LOGNAME:-$(id -un)}" \
	LANG="${LANG:-}" LC_ALL="${LC_ALL:-}" LC_CTYPE="${LC_CTYPE:-}" \
	TZ="${TZ:-}" TERM="${TERM:-dumb}" \
	ANTHROPIC_AUTH_TOKEN="$ZAI_TOKEN" \
	ANTHROPIC_BASE_URL="$ZAI_BASE" \
	${ZAI_OPUS:+ANTHROPIC_DEFAULT_OPUS_MODEL="$ZAI_OPUS"} \
	${ZAI_SONNET:+ANTHROPIC_DEFAULT_SONNET_MODEL="$ZAI_SONNET"} \
	${ZAI_HAIKU:+ANTHROPIC_DEFAULT_HAIKU_MODEL="$ZAI_HAIKU"} \
	${API_TIMEOUT:+API_TIMEOUT_MS="$API_TIMEOUT"} \
	${NONESSENTIAL:+CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC="$NONESSENTIAL"} \
	CLAUDE_CONFIG_DIR="$TMPCFG" \
	CLAUDE_CODE_AUTO_COMPACT_WINDOW="500000" \
	CLAUDE_CODE_ALWAYS_ENABLE_EFFORT="1" \
	CLAUDE_CODE_SAFE_MODE="1" \
	"$CLAUDE_BIN" -p \
		--safe-mode \
		--setting-sources "" \
		--session-id 11111111-2222-3333-4444-555555555555 \
		--name glm-isolation-smoke \
		--model opus \
		--effort high \
		--autocompact 500k \
		--output-format json \
		--dangerously-skip-permissions \
		--strict-mcp-config \
		--mcp-config '{"mcpServers":{}}' \
		--disable-slash-commands \
		--settings "$ISO_SETTINGS" \
		--append-system-prompt-file "$SYSFILE" \
		"$USER_PROMPT" \
	>"$OUTPUT_FILE" 2>"$WORK/claude.stderr"
RC=$?
set -e

echo "--- claude exit code: $RC ---"
if [[ $RC -ne 0 ]]; then
	echo "ERROR: claude起動失敗。stderr:" >&2
	cat "$WORK/claude.stderr" >&2
	exit 2
fi


RESULT="$(jq -r '.result // .error // empty' "$OUTPUT_FILE" 2>/dev/null || cat "$OUTPUT_FILE")"
echo "--- claude result ---"
printf '%s\n' "$RESULT"


PASS=1
if ! grep -q 'GLM_MARKER_OK' <<<"$RESULT"; then
	echo "FAIL: 明示prompt marker GLM_MARKER_OK が応答にありません(隔離が強すぎて明示経路も落ちた)" >&2
	PASS=0
fi
for poison in POISON_USER_GLOBAL POISON_RULES POISON_PROJECT POISON_LOCAL POISON_AUTOMEMORY POISON_AUTOMEMORY_EXTRA; do
	if grep -q "$poison" <<<"$RESULT"; then
		echo "FAIL: poison marker $poison が応答へ混入しました(隔離漏れ)" >&2
		PASS=0
	fi
done

if [[ $PASS -eq 1 ]]; then
	echo "PASS: glm-worker明示markerは応答し、poison markerは混入しません(隔離OK)"
	exit 0
fi
exit 1
