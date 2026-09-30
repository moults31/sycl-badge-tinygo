#!/bin/sh
# Print the card-assets directory the Makefile and cmd/simui should use.
#
# Card imagery is gitignored, so a git worktree never contains its own copy:
# anything dropped into one worktree is invisible to the others, and is lost
# when that worktree is removed. To keep one library across every worktree this
# resolves, in order:
#
#   1. $SYCL_CARDS_DIR, if set (point it anywhere: a backup, a NAS, a shared
#      folder);
#   2. the *main* worktree's assets/cards -- git's common dir sits in the main
#      checkout, so every linked worktree shares that one library;
#   3. ./assets/cards in the current worktree (legacy / standalone clone).
#
# It only prints a path and never creates anything. tools/make_card.py turns a
# missing manifest into the synthetic sample, so callers stay valid either way.
set -eu

if [ "${SYCL_CARDS_DIR:-}" ]; then
	printf '%s\n' "$SYCL_CARDS_DIR"
	exit 0
fi

common="$(git rev-parse --path-format=absolute --git-common-dir 2>/dev/null || true)"
if [ -z "$common" ]; then
	# Older git without --path-format: make the common dir absolute by hand.
	common="$(git rev-parse --git-common-dir 2>/dev/null || true)"
	case "$common" in
	/*) ;;
	'') ;;
	*) common="$(pwd)/$common" ;;
	esac
fi

if [ -n "$common" ]; then
	main="$(dirname "$common")"
	if [ -d "$main/assets/cards" ]; then
		printf '%s\n' "$main/assets/cards"
		exit 0
	fi
fi

printf '%s\n' "assets/cards"
