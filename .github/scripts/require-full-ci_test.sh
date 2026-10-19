#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
temporary=$(mktemp -d)
trap 'rm -rf "$temporary"' EXIT
mkdir -p "$temporary/bin"
cat > "$temporary/bin/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$TEST_DIRECTORY/calls"
case "$1 $2" in
  'run list')
    if [[ -f "$TEST_DIRECTORY/dispatched" ]]; then echo 200
    elif [[ "$SCENARIO" != missing ]]; then echo 100
    fi ;;
  'run watch') [[ "$SCENARIO" != failed ]] ;;
  'workflow run') touch "$TEST_DIRECTORY/dispatched" ;;
  api*)
    if [[ "$SCENARIO" != expired || -f "$TEST_DIRECTORY/dispatched" ]]; then echo 300; fi ;;
  *) exit 1 ;;
esac
EOF
chmod +x "$temporary/bin/gh"
export PATH="$temporary/bin:$PATH"
export GITHUB_REPOSITORY=example/denova RELEASE_TAG=v9.9.9
for SCENARIO in passed missing expired failed; do
  export SCENARIO TEST_DIRECTORY="$temporary/$SCENARIO"
  mkdir -p "$TEST_DIRECTORY"
  export GITHUB_OUTPUT="$TEST_DIRECTORY/output" GITHUB_STEP_SUMMARY="$TEST_DIRECTORY/summary"
  if [[ "$SCENARIO" == failed ]]; then
    if bash "$SCRIPT_DIR/require-full-ci.sh"; then
      echo 'Error: failed full CI was accepted' >&2
      exit 1
    fi
    test ! -f "$TEST_DIRECTORY/output"
    test ! -f "$TEST_DIRECTORY/dispatched"
  else
    bash "$SCRIPT_DIR/require-full-ci.sh"
    grep -q "sha=$(git rev-parse HEAD)" "$GITHUB_OUTPUT"
    if [[ "$SCENARIO" == passed ]]; then
      grep -q 'run-id=100' "$GITHUB_OUTPUT"
      test ! -f "$TEST_DIRECTORY/dispatched"
    else
      grep -q 'run-id=200' "$GITHUB_OUTPUT"
      grep -q 'workflow run ci.yml --ref v9.9.9 -f suite=full' "$TEST_DIRECTORY/calls"
    fi
  fi
done
echo 'Full CI release gate verified: reuse, dispatch, expired artifact and failed verification.'
