#!/bin/sh
set -u

if ! command -v glm-publication-guard >/dev/null 2>&1; then
	printf '%s\n' '{"decision":"block","reason":"current publication guard unavailable; do not bypass managed repository guards"}'
	exit 0
fi

payload=$(
	while IFS= read -r line || [ -n "$line" ]; do
		printf '%s\n' "$line"
	done
)

if ! output=$(printf '%s\n' "$payload" | glm-publication-guard pre-tool-use 2>/dev/null); then
	printf '%s\n' '{"decision":"block","reason":"current publication guard classification unavailable; do not bypass managed repository guards"}'
	exit 0
fi

if [ -n "$output" ]; then
	printf '%s\n' "$output"
fi
