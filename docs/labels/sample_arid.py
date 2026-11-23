"""Draws the sample for measuring the arid rules: every surviving mutant
the rules tag in the service repositories, and as many untagged survivors
from the same repositories, each with the source around it.

    uv run python mutant/docs/labels/sample_arid.py "$CORPUS_CACHE" > mutant/docs/labels/arid-sample.ndjson

The tagged half measures what suppressing arid mutants would hide; the
untagged half, what the rules miss.
"""

import json
import pathlib
import sys

import duckdb

DOCS = pathlib.Path(__file__).parent.parent
# yamux is left out: few of its arid mutants survive, and its kill matrix
# takes the longest to confirm.
MODULES = {
    "retryablehttp": "github.com/hashicorp/go-retryablehttp",
    "cron": "github.com/robfig/cron/v3",
}


def main(cache):
    db = duckdb.connect()
    for t in ["mutants", "results"]:
        db.sql(
            f"create view {t} as select split_part(parse_path(filename)[-2], '/', 1) repo, * exclude (filename) "
            f"from read_json_auto('{DOCS}/data-verdicts/*/{t}.ndjson.gz', filename=true)"
        )
    repos = ", ".join(f"'{r}'" for r in MODULES)
    rows = db.sql(f"""
        with survivors as (
            select m.* from mutants m
            where m.compiled and m.repo in ({repos}) and m.equivalent = ''
            and exists (select 1 from results r where r.repo = m.repo and r.mutant_id = m.id)
            and not exists (select 1 from results r where r.repo = m.repo and r.mutant_id = m.id and r.status in ('killed', 'timeout'))
        ),
        tagged as (select * from survivors where arid <> ''),
        untagged as (
            select * from survivors where arid = ''
            order by hash(repo || id::varchar || 'arid-v1')
            limit (select count(*) from tagged)
        )
        select repo, id, pkg, file, line, operator, original, mutated, arid from tagged
        union all select repo, id, pkg, file, line, operator, original, mutated, arid from untagged
        order by hash(repo || id::varchar || 'order-v1')
    """).fetchall()
    cols = ["repo", "id", "pkg", "file", "line", "operator", "original", "mutated", "arid"]
    for row in rows:
        r = dict(zip(cols, row))
        rel = r["pkg"][len(MODULES[r["repo"]]):].lstrip("/")
        src = (pathlib.Path(cache) / r["repo"] / rel / r["file"]).read_text().splitlines()
        lo, hi = max(1, r["line"] - 8), min(len(src), r["line"] + 8)
        r["context"] = "\n".join(f"{n:4d}{'>' if n == r['line'] else ' '} {src[n - 1]}" for n in range(lo, hi + 1))
        r["key"] = f"{r['repo']}#{r['id']}"
        print(json.dumps(r))


if __name__ == "__main__":
    main(sys.argv[1])
