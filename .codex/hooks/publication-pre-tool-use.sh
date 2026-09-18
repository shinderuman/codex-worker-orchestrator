#!/bin/sh
set -u

payload=$(cat)

if ! command -v glm-parent-action >/dev/null 2>&1; then
	printf '%s\n' '{"decision":"block","reason":"glm-parent-action unavailable; do not bypass managed publication guards"}'
	exit 0
fi

if ! output=$(glm-parent-action push-binding push-guard --pre-tool-use "$payload" 2>/dev/null); then
	printf '%s\n' '{"decision":"block","reason":"publication command guard unavailable; do not bypass managed publication guards"}'
	exit 0
fi

if [ -n "$output" ]; then
	printf '%s\n' "$output"
fi
