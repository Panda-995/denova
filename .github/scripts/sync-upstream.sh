#!/usr/bin/env bash
set -euo pipefail

upstream_url="${UPSTREAM_URL:-https://github.com/alfredxw/denova.git}"
upstream_branch="${UPSTREAM_BRANCH:-master}"
git fetch --no-tags "$upstream_url" "$upstream_branch"
upstream_tip="$(git rev-parse FETCH_HEAD)"
if git merge-base --is-ancestor "$upstream_tip" HEAD; then
  echo "Upstream is already synchronized."
  exit 0
fi

# Keep workflow changes out of the merge commit itself: GITHUB_TOKEN cannot
# update workflow files. Keep the fork's automation in the final merge tree.
fork_tip="$(git rev-parse HEAD)"
if ! git merge --no-commit --no-ff "$upstream_tip"; then
  git rev-parse --verify MERGE_HEAD >/dev/null
  while IFS= read -r -d '' file; do
    case "$file" in
      .github/*|Dockerfile|compose.yaml|.dockerignore|docker/*|docs/docker.md)
        side=ours ;;
      *)
        side=theirs ;;
    esac
    if git checkout "--$side" -- "$file"; then
      git add -- "$file"
    else
      # The selected side may have deleted the file.
      git rm -f -- "$file"
    fi
  done < <(git diff --name-only --diff-filter=U -z)
fi
git restore --source="$fork_tip" --staged --worktree -- .github/workflows
test -z "$(git diff --name-only --diff-filter=U)"
git commit -m "chore(sync): merge upstream ${upstream_tip:0:12}"
git push origin HEAD:master
