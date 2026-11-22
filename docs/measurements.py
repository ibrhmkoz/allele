import marimo

__generated_with = "0.25.2"
app = marimo.App(width="medium", app_title="mutant measurements")


@app.cell
def _():
    import altair as alt
    import duckdb
    import marimo as mo
    import pandas as pd

    def title(text, subtitle=""):
        return alt.TitleParams(text, subtitle=subtitle, anchor="start", fontSize=15, subtitleColor="gray")

    REPOS = ["glob", "go-humanize", "go-version", "semver", "shlex", "uuid"]
    # Each repository keeps one hue in every chart. Gray marks the reference a chart compares against.
    REPO = alt.Scale(domain=REPOS, range=["#4c78a8", "#f58518", "#54a24b", "#b279a2", "#9d755d", "#e45756"])
    PART = alt.Scale(domain=["killable", "dominators"], range=["#bab0ac", "#4c78a8"])
    PHASE = alt.Scale(domain=["build", "test runs"], range=["#72b7b2", "#bab0ac"])
    return PART, PHASE, REPO, alt, duckdb, mo, pd, title


@app.cell
def _(mo):
    mo.md(r"""
    # mutant measurements

    - **Setup:** M1 Pro 16 GiB on AC power · Go 1.27.3 · `killmatrix -j 8`, so 8 mutants build and run at once and every time below includes that contention. Measured on 2026-11-23 in one 15-minute run.
    - **Corpus:** six repositories at pinned commits (`corpus.sh`): go-version, semver, go-humanize, uuid, glob, shlex. 4 889 mutants, 677 leaf tests, 570 399 results (one per mutant and leaf). No test was flaky or failing on the unmutated code.
    - **Kill matrix:** every mutant built on its own with `go test -c -overlay`, every top-level test run against it alone; a column per leaf subtest, read from `-test.v=test2json`. A run times out after 3 × its baseline + 1 s; timeouts (8 070 runs) do not count as kills. Mutants that do not compile are left out of every score.
    - **Score:** the share of dominator mutants killed (Kurtz, Ammann et al., FSE 2016), not of all mutants. Selection strategies are simulated on the matrix, never built: tests are added at random until every mutant the strategy picked is dead, and that test set is scored on all mutants. 200 test sets per strategy, 5 seeds; seeds differ by ≤ 0.02, so dots below are means over seeds.
    """)
    return


@app.cell
def _(duckdb, mo):
    _data = str(mo.notebook_dir() / "data")
    db = duckdb.connect()
    for _t in ["mutants", "tests", "results"]:
        _ = db.sql(
            f"create view {_t} as select split_part(parse_path(filename)[-2], '/', 1) repo, * exclude (filename) "
            f"from read_json_auto('{_data}/*/{_t}.ndjson.gz', filename=true)"
        )
    summary = db.sql(f"select * from '{_data}/summary.ndjson'").df()
    strategies = db.sql(f"select * from '{_data}/strategies.ndjson'").df()
    return db, strategies, summary


@app.cell
def _(PART, alt, mo, pd, summary, title):
    _long = pd.concat(
        [
            summary.assign(part="killable", n=summary.killable),
            summary.assign(part="dominators", n=summary.dominators),
        ]
    )
    _long["label"] = _long.apply(
        lambda r: f"{r.n}" if r.part == "killable" else f"{r.n} ({r.n / r.killable:.0%})", axis=1
    )
    _bars = (
        alt.Chart(_long)
        .mark_bar()
        .encode(
            x=alt.X("n:Q", title="mutants", scale=alt.Scale(type="log", domain=[1, 1200], nice=False)),
            x2=alt.datum(1),
            y=alt.Y("repo:N", title=None),
            yOffset=alt.YOffset("part:N", sort=["killable", "dominators"]),
            color=alt.Color("part:N", scale=PART, title=None, legend=alt.Legend(orient="top")),
            tooltip=["repo", "part", "n", "mutants", "compiled", "tests", alt.Tooltip("raw_score:Q", format=".2f")],
        )
    )
    _text = _bars.mark_text(align="left", dx=4, fontSize=11).encode(text="label:N", color=alt.value("black"))
    _chart = (_bars + _text).properties(
        width=560,
        height=300,
        title=title(
            "55–947 killable mutants per repository come down to 2–37 dominators",
            "Killing the dominators kills every killable mutant. Log scale. Raw scores 0.58–0.87 count the rest again and again.",
        ),
    )
    mo.vstack(
        [
            mo.md("## 1. How much of a raw mutation score is redundancy?"),
            _chart,
            mo.md(r"""
            - **Mechanism:** an `if` with `>` yields 2 COR and 5 ROR mutants; one test usually kills most of them, so they share a kill set and count once.
            - **Limit:** dominators are relative to these tests. Tests without subtests (go-version: 33 top-level, 36 leaves) give coarse kill sets and fewer dominators than finer tests would.
            - **Decision:** every comparison below uses the dominator score. A raw score would reward strategies for keeping redundant mutants.
            """),
        ]
    )
    return


@app.cell
def _(REPO, alt, mo, pd, strategies, title):
    _s = strategies.groupby(["repo", "strategy", "control"], as_index=False)[["score", "cost", "mutants"]].mean()
    _ctl = _s[_s.control][["repo", "strategy", "score"]].rename(columns={"score": "score_random"})
    _d = _s[~_s.control].merge(_ctl, on=["repo", "strategy"])
    _d["delta"] = _d.score - _d.score_random
    _order = _d.groupby("strategy").delta.mean().sort_values(ascending=False).index.tolist()
    _pts = (
        alt.Chart(_d)
        .mark_point(filled=True, size=70, opacity=0.85)
        .encode(
            x=alt.X("delta:Q", title="dominator score minus that of as many random mutants", scale=alt.Scale(domain=[-0.4, 0.2])),
            y=alt.Y("strategy:N", sort=_order, title=None),
            color=alt.Color("repo:N", scale=REPO, title=None, legend=alt.Legend(orient="top")),
            tooltip=[
                "repo",
                "strategy",
                alt.Tooltip("score:Q", format=".2f"),
                alt.Tooltip("score_random:Q", format=".2f", title="random mutants"),
                alt.Tooltip("delta:Q", format="+.2f"),
                alt.Tooltip("cost:Q", format=".0%", title="share of cost"),
                alt.Tooltip("mutants:Q", format=".0f"),
            ],
        )
    )
    _zero = alt.Chart(pd.DataFrame({"x": [0]})).mark_rule(color="gray").encode(x="x:Q")
    _chart = (_zero + _pts).properties(
        width=560,
        height=340,
        title=title(
            "Two strategies beat random picks: one per line, and COR only",
            "Same number of mutants on both sides. Right of 0: the strategy's mutants lead to better tests. Means of 5 seeds.",
        ),
    )
    mo.vstack(
        [
            mo.md("## 2. Which mutant selection beats picking mutants at random?"),
            _chart,
            mo.md(r"""
            - **One per line:** +0.09 to +0.16 over random in 4 of 6 repos, a tie on glob and shlex.
            - **COR only:** +0.02 to +0.13 in all 6. Likely because COR flips whole branches, the faults tests are written to catch (inference, not measured).
            - **Other single operators** mostly lose: ROR only by 0.12–0.38 in 5 repos, AOR only by 0.12–0.34. Exhaustive ROR piles redundant mutants on a few comparisons.
            - **Dropping one operator** stays within ±0.07 of random except uuid without COR (−0.10): no operator is safe to drop on this corpus.
            - Matches selective vs random mutation in the literature (Zhang et al., ICSE 2010): random mutants are a strong baseline.
            """),
        ]
    )
    return


@app.cell
def _(REPO, alt, mo, strategies, title):
    _r = strategies[~strategies.control & (strategies.mutants > 0)].groupby(["repo", "strategy"], as_index=False)[["score", "cost"]].mean()
    _s = _r.groupby("strategy", as_index=False).agg(score=("score", "mean"), lo=("score", "min"), hi=("score", "max"), cost=("cost", "mean"))
    # Dropping COR, SDL, AOR or ERR keeps 80-93% of the cost; section 2 shows them, here they only crowd the corner.
    _s = _s[~_s.strategy.isin(["all but COR", "all but SDL", "all but AOR", "all but ERR"])]
    _s["picked"] = _s.strategy.isin(["one per line", "COR only"])
    _x = alt.X("cost:Q", title="mean share of the full run's build and test time", axis=alt.Axis(format="%"), scale=alt.Scale(domain=[0, 1]))
    _base = alt.Chart(_s).encode(x=_x)
    _color = alt.condition("datum.picked", alt.value("#4c78a8"), alt.value("#9e9e9e"))
    _range = _base.mark_rule(strokeWidth=2, opacity=0.5).encode(y=alt.Y("lo:Q", title="dominator score", scale=alt.Scale(domain=[0, 1])), y2="hi:Q", color=_color)
    _pts = _base.mark_point(filled=True, size=70).encode(
        y="score:Q",
        color=_color,
        tooltip=["strategy", alt.Tooltip("cost:Q", format=".0%"), alt.Tooltip("score:Q", format=".2f"), alt.Tooltip("lo:Q", format=".2f"), alt.Tooltip("hi:Q", format=".2f")],
    )
    _labels = _base.mark_text(align="left", dx=7, dy=-7, fontSize=11).encode(y="score:Q", text="strategy:N", color=_color)
    _chart = (_range + _pts + _labels).properties(
        width=560,
        height=340,
        title=title(
            "One mutant per line keeps 0.87 of the score at 36% of the cost",
            "Dot: mean over 6 repos; line: lowest to highest repo. Blue: beats random picks (section 2). Cost includes 8-way build contention.",
        ),
    )
    mo.vstack([mo.md("## 3. What does each strategy cost?"), _chart])
    return


@app.cell
def _(alt, db, mo, pd, title):
    _lines = db.sql("""
        with k as (
            select m.repo, m.pkg, m.file, m.line,
                exists (select 1 from results r where r.repo = m.repo and r.mutant_id = m.id and r.status = 'killed') killed
            from mutants m where m.compiled
        )
        select repo, count(*) lines, avg((n_killed = 0 or n_killed = n)::int) uniform
        from (select repo, pkg, file, line, count(*) n, sum(killed::int) n_killed from k group by all having count(*) > 1)
        group by repo order by repo
    """).df()
    _lines["label"] = _lines.apply(lambda r: f"{r.uniform:.0%} of {r.lines}", axis=1)
    _bars = (
        alt.Chart(_lines)
        .mark_bar(color="#4c78a8")
        .encode(
            x=alt.X("uniform:Q", title="lines whose mutants all die or all survive", axis=alt.Axis(format="%"), scale=alt.Scale(domain=[0, 1])),
            y=alt.Y("repo:N", title=None),
            tooltip=["repo", "lines", alt.Tooltip("uniform:Q", format=".1%")],
        )
    )
    _text = _bars.mark_text(align="left", dx=4, fontSize=11).encode(text="label:N", color=alt.value("black"))
    _ref = alt.Chart(pd.DataFrame({"x": [0.9], "t": ["Google: > 90%"]}))
    _rule = _ref.mark_rule(strokeDash=[4, 4], color="gray").encode(x="x:Q")
    _rule_text = _ref.mark_text(align="right", dx=-4, y=-8, color="gray").encode(x="x:Q", text="t:N")
    _chart = (_bars + _text + _rule + _rule_text).properties(
        width=560,
        height=240,
        title=title(
            "Only 22–56% of lines with several mutants die or survive together",
            "Lines with ≥ 2 compiled mutants, killed by the full suite or not. Google's figure is from its own operators and code.",
        ),
    )
    mo.vstack(
        [
            mo.md("## 4. Do the mutants of one line agree?"),
            _chart,
            mo.md(r"""
            - **Mechanism (inference, not measured):** exhaustive ROR puts 5 mutants on each comparison; the boundary one (`>` → `>=`) often survives while the rest die, so the line splits.
            - One per line still beats random (section 2). Likely because it spreads mutants over every line instead of piling them on comparisons (inference, not measured).
            - **Next:** label surviving mutants (axis D) to see how many of the splits are equivalent mutants.
            """),
        ]
    )
    return


@app.cell
def _(PHASE, alt, db, mo, title):
    _time = db.sql("""
        select repo, 'build' phase, sum(build_ms) / 1000 s from mutants group by repo
        union all
        select repo, 'test runs', sum(ms) / 1000 from (
            select distinct r.repo, r.mutant_id, split_part(t.name, '/', 1) top, r.ms
            from results r join tests t on t.repo = r.repo and t.id = r.test_id
        ) group by repo
    """).df()
    _share = _time.pivot(index="repo", columns="phase", values="s")
    _share = (_share["build"] / _share.sum(axis=1)).rename("build_share").reset_index()
    _bars = (
        alt.Chart(_time)
        .mark_bar()
        .encode(
            x=alt.X("s:Q", title="seconds, summed over mutants (8 at once)"),
            y=alt.Y("repo:N", title=None),
            color=alt.Color("phase:N", scale=PHASE, title=None, legend=alt.Legend(orient="top")),
            order=alt.Order("phase:N"),
            tooltip=["repo", "phase", alt.Tooltip("s:Q", format=".0f")],
        )
    )
    _time_chart = _bars.properties(
        width=560,
        height=200,
        title=title(
            "Builds take 26–57% of the naive runner's time",
            "One go test -c per mutant; one process per top-level test per mutant, including timeouts.",
        ),
    )

    _ops = db.sql("""
        select operator, count(*) n, avg((not compiled)::int) failed from mutants group by operator order by failed desc
    """).df()
    _ops["label"] = _ops.apply(lambda r: f"{r.failed:.1%} of {r.n}", axis=1)
    _ob = (
        alt.Chart(_ops)
        .mark_bar(color="#e45756")
        .encode(
            x=alt.X("failed:Q", title="mutants that do not compile", axis=alt.Axis(format="%"), scale=alt.Scale(domain=[0, 0.2])),
            y=alt.Y("operator:N", sort=None, title=None),
            tooltip=["operator", "n", alt.Tooltip("failed:Q", format=".1%")],
        )
    )
    _op_chart = (_ob + _ob.mark_text(align="left", dx=4, fontSize=11).encode(text="label:N", color=alt.value("black"))).properties(
        width=560,
        height=160,
        title=title(
            "COR and SDL mutants fail to compile 13.5% and 11.1% of the time",
            "All six repositories. Each still costs a build. ROR never fails.",
        ),
    )
    mo.vstack(
        [
            mo.md("## 5. Where does the naive runner's time go?"),
            _time_chart,
            _op_chart,
            mo.md(r"""
            - **Mechanism:** the mutant removes the only use of a variable, which Go rejects. `si, oi := …; if si > oi` → `if true` leaves `si` and `oi` unused (semver `version.go:780`); deleting `v[i], e = …` can do the same to `e`.
            - Cheap to avoid: with type info, skip COR when the condition holds the only use of a local, and SDL when the statement does.
            - **Schemata** (one build per package) would remove the build share; test selection by reach would cut the test runs. Both must reproduce this matrix exactly (axis A).
            """),
        ]
    )
    return


@app.cell
def _(mo):
    mo.accordion(
        {
            "Reproduce": mo.md(r"""
    From the repo root.

    ```sh
    # Kill matrices, about 15 minutes; JOBS=8 by default
    mutant/docs/corpus.sh

    # Sections 1-3: simulate the strategies on them, about 2 seconds
    go run ./mutant/cmd/simulate -o mutant/docs/data mutant/docs/data/*/

    # Sections 4-5 query the matrices directly; view everything
    make mutant-measurements
    ```
    """)
        }
    )
    return


if __name__ == "__main__":
    app.run()
