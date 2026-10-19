#!/usr/bin/env bash
set -euo pipefail

sha=$(git rev-parse HEAD)
# Push/PR smoke is not a release gate. Only full verification of this exact
# commit may supply release build inputs; a failed full run blocks publishing.
run_id=$(gh run list --workflow ci.yml --commit "$sha" --limit 30 \
  --json databaseId,event,displayTitle \
  --jq '[.[] | select((.event == "schedule" or .event == "workflow_dispatch") and (.displayTitle | startswith("Full CI (")))][0].databaseId // empty')
if [[ -n "$run_id" ]]; then
  gh run watch "$run_id" --interval 10 --exit-status
  artifact=$(gh api "repos/$GITHUB_REPOSITORY/actions/runs/$run_id/artifacts" \
    --jq '.artifacts[] | select(.name == "ci-distribution" and .expired == false) | .id')
  if [[ -z "$artifact" ]]; then run_id=''; fi
fi
if [[ -z "$run_id" ]]; then
  started=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  gh workflow run ci.yml --ref "$RELEASE_TAG" -f suite=full
  for attempt in {1..12}; do
    run_id=$(gh run list --workflow ci.yml --commit "$sha" --event workflow_dispatch \
      --created ">=$started" --json databaseId,displayTitle \
      --jq '[.[] | select(.displayTitle | startswith("Full CI ("))][0].databaseId // empty')
    if [[ -n "$run_id" ]]; then break; fi
    sleep 5
  done
  test -n "$run_id"
  gh run watch "$run_id" --interval 10 --exit-status
  # A green check without its build input cannot be used by packaging.
  gh api "repos/$GITHUB_REPOSITORY/actions/runs/$run_id/artifacts" \
    --jq '.artifacts[] | select(.name == "ci-distribution" and .expired == false) | .id' | grep -q .
fi
echo "run-id=$run_id" >> "$GITHUB_OUTPUT"
echo "sha=$sha" >> "$GITHUB_OUTPUT"
echo "Verified full CI: https://github.com/$GITHUB_REPOSITORY/actions/runs/$run_id" >> "$GITHUB_STEP_SUMMARY"
