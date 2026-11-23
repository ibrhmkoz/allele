#!/bin/sh
# Computes the kill matrix of each corpus repository at a pinned commit and
# writes it to mutant/docs/data/<name>/ as gzipped NDJSON.
#
#   mutant/docs/corpus.sh                     all repositories
#   mutant/docs/corpus.sh semver shlex        some
#   JOBS=1 OUT=mutant/docs/data-j1 mutant/docs/corpus.sh go-version
#   SCHEMATA=1 OUT=mutant/docs/data-schemata mutant/docs/corpus.sh
#
# Picked for fast, hermetic tests (no network, database or Docker) and pure
# logic: parsers, formatters, encoders. Each takes 1-5 minutes with -j 8.
set -eu

corpus='
go-version  https://github.com/hashicorp/go-version   5ca63ba49818bf0b8776df7976f57420f3ac169b
semver      https://github.com/Masterminds/semver     dd2b995c61c39ddd668b23ac919b04d607be35ab
go-humanize https://github.com/dustin/go-humanize     f5b54ae1392365a578bb43ab728e3fcdc291f0cd
uuid        https://github.com/google/uuid            2d3c2a9cc518326daf99a383f07c4d3c44317e4d
glob        https://github.com/gobwas/glob            ff0540d668f8280e0be2afab426e9c9598c80aaf
shlex       https://github.com/google/shlex           e7afc7fbc51079733e9468cdfd1efcd7d196cd1d
'

root=$(cd "$(dirname "$0")/../.." && pwd)
cache=${CORPUS_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/go-kit-mutant-corpus}
jobs=${JOBS:-8}
out_root=$(cd "$root" && mkdir -p "${OUT:-mutant/docs/data}" && cd "${OUT:-mutant/docs/data}" && pwd)
mkdir -p "$cache"
bin="$cache/killmatrix"
(cd "$root" && go build -o "$bin" ./mutant/cmd/killmatrix)

echo "$corpus" | while read -r name url commit; do
	[ -n "$name" ] || continue
	[ $# -eq 0 ] || echo " $* " | grep -q " $name " || continue
	src="$cache/$name"
	if [ ! -d "$src" ]; then
		git init -q "$src"
		git -C "$src" remote add origin "$url"
	fi
	git -C "$src" fetch -q --depth 1 origin "$commit"
	git -C "$src" checkout -q --detach "$commit"

	out="$out_root/$name"
	rm -rf "$out"
	echo "$name: computing kill matrix" >&2
	(cd "$src" && "$bin" -j "$jobs" ${SCHEMATA:+-schemata} -o "$out" ./... > /dev/null)
	gzip -9 "$out"/*.ndjson
done
