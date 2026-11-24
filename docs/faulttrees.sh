#!/bin/sh
# Prepares one source tree per real fault for cmd/bench's R: the fix
# commit's code with the package's tests as they were before the fix, the
# suite that missed the fault. A tree whose old tests do not build against
# the fixed code is dropped. Writes "<name> <pkg> <tree>" lines to stdout.
#
#   mutant/docs/faulttrees.sh mutant/docs/data-faults/*.ndjson > trees.txt
set -eu
root=$(cd "$(dirname "$0")/../.." && pwd)
cache=${CORPUS_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/go-kit-mutant-corpus}
for rows in "$@"; do
	name=$(basename "$rows" .ndjson)
	repo=$cache/$name
	[ -d "$repo/.git" ] || repo=$cache/heldout/$name
	duckdb -csv -noheader -c "select commit, pkg from read_json_auto('$rows') where status = 'ok' and coupled > 0" |
		while IFS=, read -r commit pkg; do
			tree=$cache/faults/$name-$(printf %.12s "$commit")
			if [ ! -d "$tree" ]; then
				git -C "$repo" worktree add -q --detach "$tree" "$commit"
				# Commits from before go.mod: as cmd/faults does, the module
				# at HEAD without its major version suffix, Go 1.16.
				if [ ! -f "$tree/go.mod" ]; then
					module=$(git -C "$repo" show HEAD:go.mod | sed -n 's/^module[[:space:]]*//p' | sed 's|/v[0-9]*$||')
					printf 'module %s\n\ngo 1.16\n' "$module" > "$tree/go.mod"
				fi
			fi
			# A tree serves every package of its commit; restore this one's tests
			# each time, so a second package of the commit gets its own old tests.
			parent=$(git -C "$repo" rev-parse "$commit^")
			git -C "$repo" diff --name-status --no-renames "$parent" "$commit" -- "$pkg" |
				while read -r status path; do
					case $path in
					*_test.go) ;;
					*) continue ;;
					esac
					[ "$(dirname "$path")" = "$pkg" ] || [ "$pkg" = . -a "$(dirname "$path")" = . ] || continue
					case $status in
					A) rm -f "$tree/$path" ;;
					*) git -C "$repo" show "$parent:$path" > "$tree/$path" ;;
					esac
				done
			if (cd "$tree" && go test -count=1 -run '^$' "./$pkg" > /dev/null 2>&1); then
				echo "$name $pkg $tree"
			else
				echo "$name $(printf %.12s "$commit"): old tests do not build" >&2
			fi
		done
done
