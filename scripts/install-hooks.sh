#!/bin/sh
# Wire the tracked .githooks/ directory as the git hooks path.
# Idempotent — re-running is a no-op. Same effect as `make install-hooks`.

set -e

git config core.hooksPath .githooks
chmod +x .githooks/pre-commit
echo "Hooks installed. Pre-commit runs gitleaks + go vet + gofmt + go test."
