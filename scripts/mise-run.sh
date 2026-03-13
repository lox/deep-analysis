#!/bin/bash
set -euo pipefail

function error() {
  echo "Error: $1" >&2
  exit 1
}

if [[ $# -gt 2 ]]; then
  error "Usage: mise run run -- <input> [output]"
fi

# Preserve the old INPUT/OUTPUT env-var interface while allowing positional args.
input="${INPUT:-${1:-}}"
output="${OUTPUT:-${2:-}}"

[[ -n "${OPENAI_API_KEY:-}" ]] || error "OPENAI_API_KEY environment variable is required"
[[ -n "${input}" ]] || error "Usage: mise run run -- <input> [output] or INPUT=<file> mise run run"

args=(--input "${input}")
if [[ -n "${output}" ]]; then
  args+=(--output "${output}")
fi

./dist/deep-analysis "${args[@]}"
