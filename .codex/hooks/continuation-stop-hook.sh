#!/bin/sh
set -u

if ! command -v glm-parent-action >/dev/null 2>&1; then
  printf '%s\n' '{"decision":"block","reason":"glm-parent-action unavailable; do not end USER_REQUEST"}'
  exit 0
fi

exec glm-parent-action continuation-stop-hook
