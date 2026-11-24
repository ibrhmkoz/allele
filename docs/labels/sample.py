"""Draws the hand-labeling sample (roadmap D): surviving mutants, balanced
across repositories, each with the source around it.

    uv run python docs/labels/sample.py "$CORPUS_CACHE" > docs/labels/sample.ndjson

A mutant survives when it compiles and no test kills it. covered says
whether any test reached its function (from the selection run).
"""

import json
import pathlib
import sys

import duckdb

HERE = pathlib.Path(__file__).parent
DOCS = HERE.parent
PER_REPO = {"glob": 9, "go-humanize": 9, "go-version": 9, "semver": 9, "uuid": 9, "shlex": 5}
MODULES = {
    "glob": "github.com/gobwas/glob",
    "go-humanize": "github.com/dustin/go-humanize",
    "go-version": "github.com/hashicorp/go-version",
    "semver": "github.com/Masterminds/semver/v3",
    "uuid": "github.com/google/uuid",
    "shlex": "github.com/google/shlex",
}


def main(cache):
    db = duckdb.connect()
    for name, sub in [("", "data"), ("sel_", "data-select")]:
        for t in ["mutants", "results"]:
            db.sql(
                f"create view {name}{t} as select split_part(parse_path(filename)[-2], '/', 1) repo, * exclude (filename) "
                f"from read_json_auto('{DOCS}/{sub}/*/{t}.ndjson.gz', filename=true)"
            )
    rows = db.sql("""
        with survivors as (
            select m.* from mutants m where compiled
            and not exists (select 1 from results r where r.repo = m.repo and r.mutant_id = m.id and r.status in ('killed', 'timeout'))
        ),
        covered as (
            select repo, mutant_id, bool_or(not coalesce(skipped, false)) covered from sel_results group by all
        ),
        ranked as (
            select s.*, coalesce(c.covered, false) covered,
                row_number() over (partition by s.repo order by hash(s.repo || s.id::varchar || 'labels-v1')) rank
            from survivors s left join covered c on c.repo = s.repo and c.mutant_id = s.id
        )
        select repo, id, pkg, file, line, col, operator, original, mutated, equivalent, repaired, covered, rank from ranked
        order by repo, rank
    """).fetchall()
    cols = ["repo", "id", "pkg", "file", "line", "col", "operator", "original", "mutated", "equivalent", "repaired", "covered", "rank"]
    for row in rows:
        r = dict(zip(cols, row))
        if r["rank"] > PER_REPO[r["repo"]]:
            continue
        rel = r["pkg"][len(MODULES[r["repo"]]):].lstrip("/")
        src = (pathlib.Path(cache) / r["repo"] / rel / r["file"]).read_text().splitlines()
        lo, hi = max(1, r["line"] - 8), min(len(src), r["line"] + 8)
        r["context"] = "\n".join(f"{n:4d}{'>' if n == r['line'] else ' '} {src[n - 1]}" for n in range(lo, hi + 1))
        r["key"] = f"{r['repo']}#{r['id']}"
        del r["rank"]
        print(json.dumps(r))


if __name__ == "__main__":
    main(sys.argv[1])
