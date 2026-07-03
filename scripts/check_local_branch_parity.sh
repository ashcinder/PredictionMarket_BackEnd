#!/bin/sh
set -eu

reference_branch="${1:-agent_oracle}"

if ! git rev-parse --verify "$reference_branch^{commit}" >/dev/null 2>&1; then
    echo "reference branch not found: $reference_branch" >&2
    exit 1
fi

unexpected=""
for path in $(git diff --name-only "$reference_branch" HEAD); do
    case "$path" in
        config.example.yaml|go.mod|go.sum|cmd/deploylocal/main.go|internal/chain/client.go|internal/chain/client_test.go|internal/config/config.go|internal/config/config_test.go|internal/config/runtime_profile.go|scripts/check_local_branch_parity.sh)
            ;;
        *)
            unexpected="${unexpected}${unexpected:+
}${path}"
            ;;
    esac
done

if [ -n "$unexpected" ]; then
    echo "localsupervisor contains business-logic differences from $reference_branch:" >&2
    echo "$unexpected" >&2
    exit 1
fi

echo "OK: localsupervisor differs from $reference_branch only in connection/profile files."
