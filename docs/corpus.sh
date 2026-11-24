#!/bin/sh
# Computes the kill matrix of each corpus repository at a pinned commit and
# writes it to mutant/docs/data/<name>/ as gzipped NDJSON.
#
#   mutant/docs/corpus.sh                     all repositories
#   mutant/docs/corpus.sh semver shlex        some
#   JOBS=1 OUT=mutant/docs/data-j1 mutant/docs/corpus.sh go-version
#   SCHEMATA=1 OUT=mutant/docs/data-schemata mutant/docs/corpus.sh
#   SCHEMATA=1 SELECT=1 OUT=mutant/docs/data-select mutant/docs/corpus.sh
#   SCHEMATA=1 SELECT=1 FAILFAST=1 CONFIRM=1 OUT=mutant/docs/data-verdicts mutant/docs/corpus.sh
#
# The first ten were picked for fast, hermetic tests (no network, database
# or Docker) and pure logic: parsers, formatters, encoders. Each takes 1-5
# minutes with -j 8, but decimal: 75, from 3360 mutants and 171 tests. The last three are service code that logs, with slow
# tests (25-40 s): run them with SCHEMATA=1 SELECT=1 FAILFAST=1 CONFIRM=1,
# which records each mutant's verdict but not the full matrix, and counts a
# kill only if the test fails twice: their timing tests fail under load.
set -eu

corpus='
go-version  https://github.com/hashicorp/go-version   5ca63ba49818bf0b8776df7976f57420f3ac169b
semver      https://github.com/Masterminds/semver     dd2b995c61c39ddd668b23ac919b04d607be35ab
go-humanize https://github.com/dustin/go-humanize     f5b54ae1392365a578bb43ab728e3fcdc291f0cd
uuid        https://github.com/google/uuid            2d3c2a9cc518326daf99a383f07c4d3c44317e4d
glob        https://github.com/gobwas/glob            ff0540d668f8280e0be2afab426e9c9598c80aaf
shlex       https://github.com/google/shlex           e7afc7fbc51079733e9468cdfd1efcd7d196cd1d
bloom       https://github.com/bits-and-blooms/bloom  c4c57f87f860e2451b2fc3ce3f53c6ba8fa62f6a
sjson       https://github.com/tidwall/sjson          752368dfbbb11bc8ff0e777a6e1073c3ddd3c622
decimal     https://github.com/shopspring/decimal     a5eee101f66d97f2d1bde1b6d1ebea3a93897354
xstrings    https://github.com/huandu/xstrings        01bf78f574429e544c3e334a0574936edef70f25
retryablehttp https://github.com/hashicorp/go-retryablehttp 7e8e6f42169e7dc20bf5af008698a6a06997b6e4
cron        https://github.com/robfig/cron            bc59245fe10efaed9d51b56900192527ed733435
yamux       https://github.com/hashicorp/yamux        98ba9c9ac60f4c45b2331377b50305798b41f611
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
	(cd "$src" && "$bin" -j "$jobs" ${SCHEMATA:+-schemata} ${SELECT:+-select} ${FAILFAST:+-failfast} ${CONFIRM:+-confirm} -o "$out" ./... > /dev/null)
	gzip -9 "$out"/*.ndjson
done
