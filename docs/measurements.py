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

    def span(values, fmt="{:.0f}"):
        # "a–b" over values, or one value when they agree.
        lo, hi = fmt.format(min(values)), fmt.format(max(values))
        return lo if lo == hi else f"{lo}–{hi}"

    REPOS = ["glob", "go-humanize", "go-version", "semver", "shlex", "uuid"]
    # Each repository keeps one hue in every chart. Gray marks the reference a chart compares against.
    REPO = alt.Scale(domain=REPOS, range=["#4c78a8", "#f58518", "#54a24b", "#b279a2", "#9d755d", "#e45756"])
    PART = alt.Scale(domain=["killable", "dominators"], range=["#bab0ac", "#4c78a8"])
    PHASE = alt.Scale(domain=["build", "test runs"], range=["#72b7b2", "#bab0ac"])
    FATE = alt.Scale(domain=["measured", "expected if independent"], range=["#4c78a8", "#9e9e9e"])
    RUN = alt.Scale(domain=["before repair", "after repair"], range=["#bab0ac", "#e45756"])
    return FATE, PART, PHASE, REPO, RUN, alt, duckdb, mo, pd, span, title


@app.cell
def _(duckdb, mo):
    _dir = mo.notebook_dir()
    db = duckdb.connect()
    for _name, _sub in [("", "data"), ("rerun_", "data-rerun"), ("j1_", "data-j1"), ("sch_", "data-schemata"), ("sel_", "data-select"), ("ext_", "data-extreme"), ("ver_", "data-verdicts")]:
        for _t in ["mutants", "tests", "results"]:
            _ = db.sql(
                f"create view {_name}{_t} as select split_part(parse_path(filename)[-2], '/', 1) repo, * exclude (filename) "
                f"from read_json_auto('{_dir}/{_sub}/*/{_t}.ndjson.gz', filename=true)"
            )
    summary = db.sql(f"select * from '{_dir}/data/summary.ndjson' order by repo").df()
    strategies = db.sql(f"select * from '{_dir}/data/strategies.ndjson'").df()
    totals = db.sql("""
        select (select count(*) from mutants) mutants, (select count(*) from mutants where compiled) compiled,
            (select count(*) from mutants where repaired) repaired, (select count(*) from tests) leaves,
            (select count(*) from tests where state <> 'ok') excluded, (select count(*) from results) results,
            (select count(*) from results where status = 'timeout') timeouts
    """).df().iloc[0]
    return db, strategies, summary, totals


@app.cell
def _(mo, totals):
    mo.md(rf"""
    # mutant measurements

    - **Setup:** M1 Pro 16 GiB (AC power), Go 1.27.3, `killmatrix -j 8` (8 mutants in parallel; §6 compares `-j 1`). Measured 2026-11-23: two full runs (`data/`, `data-rerun/`), schemata (`data-schemata/`, §8), schemata + selection (`data-select/`, §9), and `-j 1` on two repos (`data-j1/`).
    - **Corpus:** 6 pinned repos (`corpus.sh`): glob, go-humanize, go-version, semver, shlex, uuid. {totals.mutants:,} mutants ({totals.compiled:,} compile, {totals.repaired} repaired), {totals.leaves} leaf tests ({totals.excluded} flaky/failing), {totals.results:,} results.
    - **Kill matrix:** Each mutant builds alone (`go test -c -vet=off -overlay`) and runs each top-level test alone. Columns are leaf subtests (`-test.v=test2json`). Timeout = 3× baseline + 1 s ({totals.timeouts:,} timeouts, not counted as kills). Build errors are excluded from scores.
    - **Score:** Share of dominator mutants killed (Kurtz et al., FSE 2016). Strategies are simulated on the matrix: add random tests until every kept mutant dies, then score on all mutants. Each strategy is paired with a random control of equal size (200 test sets × 30 seeds).
    """)
    return


@app.cell
def _(PART, alt, mo, pd, span, summary, title):
    _long = pd.concat(
        [
            summary.assign(part="killable", n=summary.killable),
            summary.assign(part="dominators", n=summary.dominators),
        ]
    )
    _long["label"] = _long.apply(lambda r: f"{r.n}" if r.part == "killable" else f"{r.n} ({r.n / r.killable:.0%})", axis=1)
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
            f"{span(summary.killable)} killable mutants reduce to {span(summary.dominators)} dominators",
            f"Killing the dominators kills all killable mutants. Log scale. Raw scores ({span(summary.raw_score, '{:.2f}')}) double-count the rest.",
        ),
    )
    mo.vstack(
        [
            mo.md("## 1. How much of a raw score is redundancy?"),
            _chart,
            mo.md(r"""
            - **Why:** One `if` with `>` creates 2 COR and 5 ROR mutants. One test usually kills most of them, so they share a kill set and count once.
            - **Limit:** Dominators depend on the test suite. Few dominators make scores coarse: `shlex` has 2, so scores can only be 0, 0.5, or 1. §2 shows each repo's count.
            """),
        ]
    )
    return


@app.cell
def _(REPO, alt, mo, pd, strategies, summary, title):
    _p = strategies[~strategies.control].merge(
        strategies[strategies.control][["repo", "strategy", "seed", "score"]].rename(columns={"score": "score_random"}),
        on=["repo", "strategy", "seed"],
    )
    _p["delta"] = _p.score - _p.score_random
    _d = _p.groupby(["repo", "strategy"], as_index=False).agg(
        delta=("delta", "mean"),
        lo=("delta", lambda x: x.quantile(0.025)),
        hi=("delta", lambda x: x.quantile(0.975)),
        score=("score", "mean"),
        score_random=("score_random", "mean"),
        cost=("cost", "mean"),
        mutants=("mutants", "mean"),
    )
    _doms = dict(zip(summary.repo, summary.dominators))
    _d["dominators"] = _d.repo.map(_doms)
    _order = _d.groupby("strategy").delta.mean().sort_values(ascending=False).index.tolist()
    _y = alt.Y("strategy:N", sort=_order, title=None)
    _off = alt.YOffset("repo:N")
    _color = alt.Color(
        "repo:N",
        scale=REPO,
        title=None,
        legend=alt.Legend(orient="top", labelExpr="datum.label + ' (' + " + str(_doms).replace("'", '"') + "[datum.label] + ')'"),
    )
    _rules = alt.Chart(_d).mark_rule(strokeWidth=2).encode(x=alt.X("lo:Q", title="dominator score minus that of as many random mutants (95% of 30 seeds)", scale=alt.Scale(domain=[-0.45, 0.25])), x2="hi:Q", y=_y, yOffset=_off, color=_color)
    _pts = (
        alt.Chart(_d)
        .mark_point(filled=True, size=40)
        .encode(
            x="delta:Q",
            y=_y,
            yOffset=_off,
            color=_color,
            tooltip=[
                "repo",
                "dominators",
                "strategy",
                alt.Tooltip("delta:Q", format="+.3f"),
                alt.Tooltip("lo:Q", format="+.3f"),
                alt.Tooltip("hi:Q", format="+.3f"),
                alt.Tooltip("score:Q", format=".2f"),
                alt.Tooltip("score_random:Q", format=".2f", title="random mutants"),
                alt.Tooltip("cost:Q", format=".0%", title="share of cost"),
                alt.Tooltip("mutants:Q", format=".0f"),
            ],
        )
    )
    _zero = alt.Chart(pd.DataFrame({"x": [0]})).mark_rule(color="gray").encode(x="x:Q")
    _opl = _d[_d.strategy == "one per line"]
    _cor = _d[_d.strategy == "COR only"]
    _opl_wins = int((_opl.lo > 0).sum())
    _cor_wins = int((_cor.lo > 0).sum())
    _chart = (_zero + _rules + _pts).properties(
        width=560,
        height=620,
        title=title(
            f"One per line beats random in {_opl_wins}/6 repos; COR alone in {_cor_wins}/6",
            "Dot: 30-seed mean; bar: 95% range. Right of 0 beats random. Legend shows dominator count.",
        ),
    )
    _ties = ", ".join(f"{r.repo} ({r.dominators} dominators, {r.lo:+.3f} to {r.hi:+.3f})" for r in _opl[_opl.lo <= 0].itertuples())
    mo.vstack(
        [
            mo.md("## 2. Which strategies beat random picks?"),
            _chart,
            mo.md(rf"""
            - **One per line** ties random in {_ties}. `shlex` has only 2 dominators, so all strategies tie there. `glob` is a real tie (interval crosses 0).
            - **COR only:** Likely wins because it flips whole branches (not measured directly).
            - **Other single operators** mostly lose. ROR piles redundant mutants on a few lines; AOR and ERR cover too few lines.
            - **All but repaired** loses to random in most repos. Dropping repaired mutants loses more dominators than dropping random ones (§5).
            - **Seeds** vary only the simulation; other tests or repos would shift the dots (see Zhang et al., ICSE 2010).
            """),
        ]
    )
    return


@app.cell
def _(alt, mo, strategies, title):
    _r = strategies[~strategies.control & (strategies.mutants > 0)].groupby(["repo", "strategy"], as_index=False)[["score", "cost"]].mean()
    _s = _r.groupby("strategy", as_index=False).agg(score=("score", "mean"), lo=("score", "min"), hi=("score", "max"), cost=("cost", "mean"))
    # Dropping one operator, the repaired or the equivalent mutants keeps 78-99% of the cost; section 2 shows them.
    _s = _s[~_s.strategy.str.startswith("all but") | (_s.strategy == "all but ROR")]
    _s["picked"] = _s.strategy.isin(["one per line", "COR only"])
    _base = alt.Chart(_s).encode(x=alt.X("cost:Q", title="mean share of the full run's build and test time", axis=alt.Axis(format="%"), scale=alt.Scale(domain=[0, 1])))
    _color = alt.condition("datum.picked", alt.value("#4c78a8"), alt.value("#9e9e9e"))
    _range = _base.mark_rule(strokeWidth=2, opacity=0.5).encode(y=alt.Y("lo:Q", title="dominator score", scale=alt.Scale(domain=[0, 1])), y2="hi:Q", color=_color)
    _pts = _base.mark_point(filled=True, size=70).encode(
        y="score:Q",
        color=_color,
        tooltip=["strategy", alt.Tooltip("cost:Q", format=".0%"), alt.Tooltip("score:Q", format=".2f"), alt.Tooltip("lo:Q", format=".2f"), alt.Tooltip("hi:Q", format=".2f")],
    )
    _labels = _base.mark_text(align="left", dx=7, dy=-7, fontSize=11).encode(y="score:Q", text="strategy:N", color=_color)
    _opl = _s[_s.strategy == "one per line"].iloc[0]
    _chart = (_range + _pts + _labels).properties(
        width=560,
        height=340,
        title=title(
            f"One per line keeps {_opl.score:.2f} of the score at {_opl.cost:.0%} of the cost",
            "Dot: 6-repo mean; bar: min–max. Blue beats random (§2). Cost includes 8-way build contention.",
        ),
    )
    mo.vstack([mo.md("## 3. What does each strategy cost?"), _chart])
    return


@app.cell
def _(FATE, alt, db, mo, pd, span, title):
    # k: per compiled mutant, whether the suite killed it, killed it or timed out, and its kill vector.
    _ = db.sql("""
        create or replace temp table k as
        select m.repo, m.pkg, m.file, m.line, m.id, m.equivalent <> '' eq,
            coalesce(bool_or(r.status = 'killed'), false) killed,
            coalesce(bool_or(r.status in ('killed', 'timeout')), false) killed_or_timeout,
            coalesce(string_agg(r.test_id::varchar, ',' order by r.test_id) filter (where r.status = 'killed'), '') vec
        from mutants m left join results r on r.repo = m.repo and r.mutant_id = m.id
        where m.compiled group by all
    """)
    _fate = db.sql("""
        with lng as (
            select 'all share the fate' def, repo, pkg, file, line, killed o from k
            union all select 'timeouts count as killed', repo, pkg, file, line, killed_or_timeout from k
            union all select 'equivalent-tagged left out', repo, pkg, file, line, killed from k where not eq
        ),
        lines as (select def, repo, pkg, file, line, count(*) n, sum(o::int) s from lng group by all having count(*) >= 2),
        rate as (select def, repo, avg(o::int) p from lng group by all)
        select def, repo, count(*) lines, avg((s = 0 or s = n)::int) measured, avg(pow(p, n) + pow(1 - p, n)) expected, avg(greatest(s, n - s) / n) majority
        from lines join rate using (def, repo) group by all order by def, repo
    """).df()
    _vec = db.sql("""
        select repo, avg((v = 1)::int) same_vector
        from (select repo, pkg, file, line, count(distinct vec) v from k group by all having count(*) >= 2) group by repo order by repo
    """).df()
    _main = _fate[_fate["def"] == "all share the fate"]
    _long = pd.concat(
        [
            _main.assign(kind="measured", share=_main.measured),
            _main.assign(kind="expected if independent", share=_main.expected),
        ]
    )
    _pts = (
        alt.Chart(_long)
        .mark_point(filled=True, size=90)
        .encode(
            x=alt.X("share:Q", title="lines whose mutants all die or all survive", axis=alt.Axis(format="%"), scale=alt.Scale(domain=[0, 1])),
            y=alt.Y("repo:N", title=None),
            color=alt.Color("kind:N", scale=FATE, title=None, legend=alt.Legend(orient="top")),
            tooltip=["repo", "kind", "lines", alt.Tooltip("share:Q", format=".1%")],
        )
    )
    _link = alt.Chart(_main).mark_rule(color="#bab0ac").encode(x="expected:Q", x2="measured:Q", y="repo:N")
    _ref = alt.Chart(pd.DataFrame({"x": [0.9], "t": ["Google: > 90%"]}))
    _rule = _ref.mark_rule(strokeDash=[4, 4], color="gray").encode(x="x:Q")
    _rule_text = _ref.mark_text(align="right", dx=-4, y=-8, color="gray").encode(x="x:Q", text="t:N")
    _chart = (_link + _pts + _rule + _rule_text).properties(
        width=560,
        height=240,
        title=title(
            f"{span(_main.measured * 100)}% of lines share one fate; {span(_main.expected * 100)}% would by chance",
            "Lines with ≥ 2 compiled mutants. Expected: pⁿ + (1 − p)ⁿ using each repo's kill rate p.",
        ),
    )
    _table = _fate.pivot(index="repo", columns="def", values="measured")[["all share the fate", "timeouts count as killed", "equivalent-tagged left out"]]
    _table = _table.join(_vec.set_index("repo")).join(_main.set_index("repo")[["majority", "expected", "lines"]])
    _table = _table.rename(columns={"same_vector": "same kill vector", "majority": "mean majority fate", "expected": "expected if independent"})
    _cols = [c for c in _table.columns if c != "lines"]
    _md = "| repo | lines | " + " | ".join(_cols) + " |\n|---|---:|" + "---:|" * len(_cols) + "\n"
    for _repo, _row in _table.iterrows():
        _md += f"| {_repo} | {_row.lines:.0f} | " + " | ".join(f"{_row[c]:.0%}" for c in _cols) + " |\n"
    mo.vstack(
        [
            mo.md("## 4. Do mutants on the same line share their fate?"),
            _chart,
            mo.md(_md),
            mo.md(r"""
            - **Google baseline:** Petrović et al. (ICSE 2021) found >90% of bug-fix lines had all mutants killed or all survive, using Google's operators and arid filtering.
            - **Definitions:** First column matches Google's (killed vs not, build errors dropped, ≥2 mutants/line). *Same kill vector* is strictest (exact same tests kill all); *mean majority fate* is loosest.
            - **Test granularity matters:** Fine-grained suites (`glob`, `uuid`) agree ~2× more than chance. `go-version` (33 top-level tests, few subtests) sits near chance.
            - **Why lower than Google (likely):** Exhaustive ROR puts 5 mutants per comparison, and boundary mutants (`>` → `>=`) often survive alone. Google also makes fewer mutants per line.
            """),
        ]
    )
    return


@app.cell
def _(RUN, alt, db, mo, pd, summary, title):
    # Before repair: the first corpus run (commit 8cb5706), the same mutants without repairs, built with vet.
    _before = pd.DataFrame(
        [("COR", 1131, 0.135), ("SDL", 674, 0.111), ("AOR", 801, 0.022), ("ERR", 242, 0.012), ("ROR", 2041, 0.0)],
        columns=["operator", "n", "failed"],
    ).assign(run="before repair")
    _after = db.sql("select operator, count(*) n, avg((not compiled)::int) failed from mutants group by operator").df().assign(run="after repair")
    _ops = pd.concat([_before, _after])
    _ops["label"] = _ops.apply(lambda r: f"{r.failed:.1%}", axis=1)
    _bars = (
        alt.Chart(_ops)
        .mark_bar()
        .encode(
            x=alt.X("failed:Q", title="mutants that do not compile", axis=alt.Axis(format="%"), scale=alt.Scale(domain=[0, 0.16])),
            y=alt.Y("operator:N", sort=["COR", "SDL", "AOR", "ERR", "ROR"], title=None),
            yOffset=alt.YOffset("run:N", sort=["before repair", "after repair"]),
            color=alt.Color("run:N", scale=RUN, title=None, legend=alt.Legend(orient="top")),
            tooltip=["operator", "run", "n", alt.Tooltip("failed:Q", format=".1%")],
        )
    )
    _failed_after = int(db.sql("select count(*) from mutants where not compiled").fetchall()[0][0])
    _op_chart = (_bars + _bars.mark_text(align="left", dx=4, fontSize=10).encode(text="label:N", color=alt.value("black"))).properties(
        width=560,
        height=240,
        title=title(
            f"Build repair cuts compile errors from 249 to {_failed_after} (of 4,889 mutants)",
            "Adds _ = x for unused vars and _ = pkg.Name for imports in COR, SDL, ERR. Builds also skip vet.",
        ),
    )

    _eq = db.sql("""
        select count(*) tagged,
            count(*) filter (where exists (select 1 from results r where r.repo = m.repo and r.mutant_id = m.id and r.status in ('killed', 'timeout'))) killed
        from mutants m where equivalent <> '' and compiled
    """).fetchall()[0]
    _surv = int(db.sql("""
        select count(*) from mutants m where compiled
        and not exists (select 1 from results r where r.repo = m.repo and r.mutant_id = m.id and r.status = 'killed')
    """).fetchall()[0][0])
    _gain = summary.assign(gained=summary.dominators - summary.dominators_unrepaired)
    _gain_txt = ", ".join(f"{r.repo} {r.dominators_unrepaired}→{r.dominators}" for r in _gain[_gain.gained != 0].itertuples())
    mo.vstack(
        [
            mo.md("## 5. Do build repair and equivalence rules pay off?"),
            _op_chart,
            mo.md(rf"""
            - **Repair vs drop:** {int(summary.repaired.sum())} mutants needed repair. Dropping them loses dominators ({_gain_txt}) and loses to random (§2).
            - **Still failing:** Constant-overflow AOR (`TByte * 1000` → `-`), ERR returning `nil` for non-nilable types, and type/generic-only imports (type-only imports were fixed later with `(*pkg.T)(nil)`).
            - **Equivalence rule** (non-negative value vs 0: `!=` → `>`, `==` → `<=`): {_eq[0]} tagged, {_eq[1]} killed or timed out. Catches {_eq[0]}/{_surv:,} survivors ({_eq[0] / _surv:.0%})—cheap, small win.
            """),
        ]
    )
    return


@app.cell
def _(PHASE, alt, db, mo, pd, span, title):
    _cost = db.sql("""
        with runs as (
            select 'j8' jobs, r.repo, r.mutant_id, split_part(t.name, '/', 1) top, any_value(r.ms) ms, bool_or(r.status = 'timeout') tmo
            from results r join tests t on t.repo = r.repo and t.id = r.test_id group by all
            union all
            select 'j1', r.repo, r.mutant_id, split_part(t.name, '/', 1), any_value(r.ms), bool_or(r.status = 'timeout')
            from j1_results r join j1_tests t on t.repo = r.repo and t.id = r.test_id group by all
        ),
        builds as (
            select 'j8' jobs, repo, sum(build_ms) b from mutants group by all
            union all select 'j1', repo, sum(build_ms) from j1_mutants group by all
        )
        select jobs, repo, any_value(b) / 1000 build_s, sum(ms) / 1000 run_s, sum(ms) filter (where tmo) / sum(ms) timeout_share
        from runs join builds using (jobs, repo) group by all
    """).df()
    _cost["build_share"] = _cost.build_s / (_cost.build_s + _cost.run_s)
    _j8 = _cost[_cost.jobs == "j8"]
    _long = pd.concat([_j8.assign(phase="build", s=_j8.build_s), _j8.assign(phase="test runs", s=_j8.run_s)])
    _bars = (
        alt.Chart(_long)
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
            f"Builds take {span(_j8.build_share * 100)}% of naive runner time at -j 8",
            "One go test -c per mutant; one process per top-level test per mutant.",
        ),
    )
    _both = _cost[_cost.repo.isin(_cost[_cost.jobs == "j1"].repo)]
    _both = _both.assign(setting=_both.jobs.map({"j1": "-j 1", "j8": "-j 8"}))
    _jb = (
        alt.Chart(_both)
        .mark_bar()
        .encode(
            x=alt.X("build_share:Q", title="build share of time", axis=alt.Axis(format="%"), scale=alt.Scale(domain=[0, 1])),
            y=alt.Y("repo:N", title=None),
            yOffset=alt.YOffset("setting:N", sort=["-j 1", "-j 8"]),
            color=alt.Color("setting:N", scale=alt.Scale(domain=["-j 1", "-j 8"], range=["#72b7b2", "#bab0ac"]), title=None, legend=alt.Legend(orient="top")),
            tooltip=["repo", "setting", alt.Tooltip("build_s:Q", format=".0f"), alt.Tooltip("run_s:Q", format=".0f"), alt.Tooltip("build_share:Q", format=".0%")],
        )
    )
    _j1 = _cost[_cost.jobs == "j1"]
    _j_chart = (_jb + _jb.mark_text(align="left", dx=4, fontSize=10).encode(text=alt.Text("build_share:Q", format=".0%"), color=alt.value("black"))).properties(
        width=560,
        height=120,
        title=title(
            f"Without contention, builds are {span(_j1.build_share * 100)}% of time",
            "go-version and shlex rerun with -j 1.",
        ),
    )
    mo.vstack(
        [
            mo.md("## 6. Where does the naive runner's time go?"),
            _time_chart,
            _j_chart,
            mo.md(rf"""
            - **Timeouts:** Under 0.5% of runs, but {span(_j1.timeout_share * 100)}% of test time at `-j 1` (each waits 3× baseline + 1 s).
            - **Next cuts:** Schemata removes build time; test selection cuts test runs. Both must match this matrix (§7 sets the noise floor).
            """),
        ]
    )
    return


@app.cell
def _(db, mo):
    _cells = """
        select m.repo, m.pkg, m.file, m.line, m.col, m.mutated, t.name, r.status
        from {p}mutants m join {p}results r on r.repo = m.repo and r.mutant_id = m.id join {p}tests t on t.repo = r.repo and t.id = r.test_id
    """
    _ = db.sql(f"""
        create or replace temp table diff as
        select a.repo, a.pkg, a.file, a.line, a.col, a.mutated, a.name, a.status a_status, b.status b_status
        from ({_cells.format(p="")}) a join ({_cells.format(p="rerun_")}) b using (repo, pkg, file, line, col, mutated, name)
    """)
    _cmp = db.sql("""
        select count(*), count(*) filter (where a_status <> b_status),
            count(*) filter (where a_status <> b_status and 'timeout' in (a_status, b_status)),
            count(distinct (repo, pkg, file, line, col, mutated)) filter (where a_status <> b_status and 'timeout' not in (a_status, b_status))
        from diff
    """).fetchall()[0]
    _flips = db.sql("""
        select repo, file || ':' || line loc, left(mutated, 40) mutated, count(*) cells from diff
        where a_status <> b_status and 'timeout' not in (a_status, b_status) group by all order by cells desc, loc
    """).df()
    _j = db.sql("select count(*), count(*) filter (where a.status <> b.status) from results a join j1_results b using (repo, mutant_id, test_id)").fetchall()[0]
    _md = "| repo | where | mutant | cells |\n|---|---|---|---:|\n" + "".join(f"| {r.repo} | `{r.loc}` | `{r.mutated}` | {r.cells} |\n" for r in _flips.itertuples())
    mo.vstack(
        [
            mo.md("## 7. How reproducible is the naive matrix?"),
            mo.md(rf"""
            Two `-j 8` runs agree on {_cmp[0] - _cmp[1]:,} of {_cmp[0]:,} cells ({1 - _cmp[1] / _cmp[0]:.3%}). Of the {_cmp[1]} diffs, {_cmp[2]} hit the timeout boundary. The rest come from {_cmp[3]} flaky mutants:
            """),
            mo.md(_md),
            mo.md(rf"""
            - `-j 1` and `-j 8` agree on all but {_j[1]} of {_j[0]:,} cells (`go-version`, `shlex`). Parallelism does not change kills there.
            - **Takeaway for schemata:** Rerun disagreeing cells before calling them bugs.
            - **Likely causes:** `glob` pools arenas with `sync.Pool` (GC timing); `uuid` v6/v7 reads the system clock.
            """),
        ]
    )
    return


@app.cell
def _(PHASE, alt, db, mo, pd, title):
    _cells = """
        select r.repo, r.mutant_id, r.test_id, r.status from {p}results r
    """
    _ = db.sql(f"""
        create or replace temp table ab as
        select a.repo, a.mutant_id, a.test_id, a.status naive, b.status sch, c.status rerun
        from ({_cells.format(p="")}) a
        join ({_cells.format(p="sch_")}) b using (repo, mutant_id, test_id)
        left join ({_cells.format(p="rerun_")}) c using (repo, mutant_id, test_id)
    """)
    _same_ids = db.sql("select count(*) from mutants a join sch_mutants b using (repo, id) where a.mutated <> b.mutated").fetchall()[0][0]
    _kinds = db.sql("""
        select count(*) cells,
            count(*) filter (where naive <> sch) differ,
            count(*) filter (where naive <> sch and 'timeout' in (naive, sch)) timeouts,
            count(*) filter (where naive <> sch and 'timeout' not in (naive, sch) and naive <> rerun) naive_flips,
            count(*) filter (where naive <> sch and 'timeout' not in (naive, sch) and naive = rerun) other,
            count(*) filter (where naive <> rerun) noise
        from ab
    """).fetchall()[0]
    _other = db.sql("""
        select m.repo, m.file || ':' || m.line loc, m.operator, left(m.mutated, 40) mutated, ab.naive, ab.sch, count(*) cells
        from ab join sch_mutants m on m.repo = ab.repo and m.id = ab.mutant_id
        where naive <> sch and 'timeout' not in (naive, sch) and naive = rerun
        group by all order by cells desc, loc
    """).df()
    _emb = db.sql("select avg(schema::int) from sch_mutants").fetchall()[0][0]
    _cost = db.sql("""
        with runs as (
            select 'build per mutant' setup, r.repo, r.mutant_id, split_part(t.name, '/', 1) top, any_value(r.ms) ms
            from results r join tests t on t.repo = r.repo and t.id = r.test_id group by all
            union all
            select 'schemata', r.repo, r.mutant_id, split_part(t.name, '/', 1), any_value(r.ms)
            from sch_results r join sch_tests t on t.repo = r.repo and t.id = r.test_id group by all
        ),
        builds as (
            select 'build per mutant' setup, repo, sum(build_ms) b from mutants group by all
            union all select 'schemata', repo, sum(build_ms) from sch_mutants group by all
        )
        select setup, repo, any_value(b) / 1000 build_s, sum(ms) / 1000 run_s from runs join builds using (setup, repo) group by all
    """).df()
    _tot = _cost.groupby("setup")[["build_s", "run_s"]].sum()
    _long = pd.concat([_cost.assign(phase="build", s=_cost.build_s), _cost.assign(phase="test runs", s=_cost.run_s)])
    _bars = (
        alt.Chart(_long)
        .mark_bar()
        .encode(
            x=alt.X("s:Q", title="seconds, summed over mutants (8 at once)"),
            y=alt.Y("repo:N", title=None),
            yOffset=alt.YOffset("setup:N", sort=["build per mutant", "schemata"]),
            color=alt.Color("phase:N", scale=PHASE, title=None, legend=alt.Legend(orient="top")),
            opacity=alt.Opacity("setup:N", scale=alt.Scale(domain=["build per mutant", "schemata"], range=[0.45, 1.0]), title=None),
            order=alt.Order("phase:N"),
            tooltip=["repo", "setup", "phase", alt.Tooltip("s:Q", format=".0f")],
        )
    )
    _b, _s = _tot.loc["build per mutant"], _tot.loc["schemata"]
    _chart = _bars.properties(
        width=560,
        height=300,
        title=title(
            f"Schemata cuts build time {_b.build_s / _s.build_s:.0f}× ({_b.build_s / 60:.0f} → {_s.build_s / 60:.1f} min)",
            f"Faded: build per mutant; solid: schemata. {_emb:.1%} embedded. Test runs: {_b.run_s / 60:.0f} vs {_s.run_s / 60:.0f} min.",
        ),
    )
    _md = "| repo | where | operator | mutant | build per mutant | schemata | cells |\n|---|---|---|---|---|---|---:|\n" + "".join(
        f"| {r.repo} | `{r.loc}` | {r.operator} | `{r.mutated}` | {r.naive} | {r.sch} | {r.cells} |\n" for r in _other.itertuples()
    )
    mo.vstack(
        [
            mo.md("## 8. Does schemata keep the matrix, and how much faster is it?"),
            mo.md(rf"""
            Schemata matches build-per-mutant on {_kinds[0] - _kinds[1]:,} of {_kinds[0]:,} cells ({1 - _kinds[1] / _kinds[0]:.3%}); mutant IDs match 1-to-1 ({_same_ids} text diffs). Of {_kinds[1]} diffs, {_kinds[2]} are timeouts and {_kinds[3]} also flip between naive reruns. That leaves {_kinds[4]} (vs {_kinds[5]} noise between naive reruns, §7).
            """),
            mo.md(_md) if len(_other) else mo.md("No other cell differs."),
            _chart,
            mo.md(r"""
            - **Bug caught:** First run disagreed on `semver/constraints.go:503` and `go-humanize/bytes.go:174`. Text swap of `&&` → `||` broke precedence (`a || b && c`). `Find` now adds parentheses.
            - **Remaining diffs:** All sit on the flaky lines from §7 (`uuid` clock, `glob` pool, `go-humanize/si.go`).
            - **Faster tests too:** Less CPU contention from builds on 8 cores.
            - **Wall clock (`-j 8`):** ~15 min → ~6 min across the corpus.
            """),
        ]
    )
    return


@app.cell
def _(alt, db, mo, pd, title):
    _ = db.sql("""
        create or replace temp table sel as
        select a.repo, a.mutant_id, a.test_id, a.status naive, s.status sel, coalesce(s.skipped, false) skipped, c.status rerun
        from results a join sel_results s using (repo, mutant_id, test_id)
        left join rerun_results c using (repo, mutant_id, test_id)
    """)
    _k = db.sql("""
        select count(*), count(*) filter (where skipped),
            count(*) filter (where skipped and naive <> sel),
            count(*) filter (where skipped and naive <> sel and naive = rerun),
            count(*) filter (where naive <> sel),
            count(*) filter (where naive <> sel and 'timeout' not in (naive, sel) and naive = rerun)
        from sel
    """).fetchall()[0]
    _runs = db.sql("""
        with runs as (
            select 'build per mutant' setup, r.repo, r.mutant_id, split_part(t.name, '/', 1) top, any_value(r.ms) ms, false sk
            from results r join tests t on t.repo = r.repo and t.id = r.test_id group by all
            union all
            select 'schemata', r.repo, r.mutant_id, split_part(t.name, '/', 1), any_value(r.ms), false
            from sch_results r join sch_tests t on t.repo = r.repo and t.id = r.test_id group by all
            union all
            select 'schemata + selection', r.repo, r.mutant_id, split_part(t.name, '/', 1), any_value(r.ms), bool_and(coalesce(r.skipped, false))
            from sel_results r join sel_tests t on t.repo = r.repo and t.id = r.test_id group by all
        ),
        builds as (
            select 'build per mutant' setup, sum(build_ms) b from mutants
            union all select 'schemata', sum(build_ms) from sch_mutants
            union all select 'schemata + selection', sum(build_ms) from sel_mutants
        )
        select setup, count(*) filter (where not sk) processes, sum(ms) filter (where not sk) / 60000 run_min, any_value(b) / 60000 build_min
        from runs join builds using (setup) group by setup
    """).df()
    # Wall clock of one corpus.sh run at -j 8, from its time output.
    _wall = {"build per mutant": 14.6, "schemata": 5.9, "schemata + selection": 2.9}
    _runs["wall_min"] = _runs.setup.map(_wall)
    _order = ["build per mutant", "schemata", "schemata + selection"]
    _bars = (
        alt.Chart(_runs)
        .mark_bar(color="#4c78a8")
        .encode(
            x=alt.X("wall_min:Q", title="minutes for the whole corpus, -j 8"),
            y=alt.Y("setup:N", sort=_order, title=None),
            color=alt.condition("datum.setup == 'build per mutant'", alt.value("#9e9e9e"), alt.value("#4c78a8")),
            tooltip=["setup", alt.Tooltip("wall_min:Q", format=".1f"), "processes", alt.Tooltip("run_min:Q", format=".0f", title="test-run minutes"), alt.Tooltip("build_min:Q", format=".1f", title="build minutes")],
        )
    )
    _base = _wall["build per mutant"]
    _runs["label"] = _runs.apply(lambda r: f"{r.wall_min:.1f} min" + ("" if r.setup == "build per mutant" else f" ({_base / r.wall_min:.1f}× faster)"), axis=1)
    _text = alt.Chart(_runs).mark_text(align="left", dx=4, fontSize=11).encode(x="wall_min:Q", y=alt.Y("setup:N", sort=_order), text="label:N")
    _proc = dict(zip(_runs.setup, _runs.processes))
    _chart = (_bars + _text).properties(
        width=520,
        height=130,
        title=title(
            f"Schemata + test selection runs {_base / _wall['schemata + selection']:.0f}× faster",
            f"Test processes: {_proc['build per mutant']:,} → {_proc['schemata + selection']:,}. Same matrix (below).",
        ),
    )
    mo.vstack(
        [
            mo.md("## 9. Does test selection keep the matrix?"),
            _chart,
            mo.md(rf"""
            - **Skipped:** {_k[1]:,} of {_k[0]:,} cells ({_k[1] / _k[0]:.0%}) where the test never reached the mutant's function in the baseline. {_k[2]} skipped cells differ ({_k[2] - _k[3]} also flip in naive reruns).
            - **Function-level, not site-level:** Site-level selection missed 6 real kills in `uuid`'s `getV7Time`, where a clock-collision branch never ran in baseline but did run under mutants.
            - **Overall:** {_k[4]} diffs total; {_k[5]} non-timeout, non-noise diffs—all on §7's flaky lines or non-schema mutants.
            - **Fallback:** Mutants outside functions or not embedded run all tests.
            """),
        ]
    )
    return


@app.cell
def _(alt, db, mo, pd, title):
    _dir = mo.notebook_dir() / "data-faults"
    _f = db.sql(f"select * from read_json_auto('{_dir}/*.ndjson', union_by_name=true)").df()
    _ok = _f[_f.status == "ok"].copy()
    _ctl = _ok[_ok.chance >= 0]
    _rows = pd.DataFrame(
        [
            ("any line", "fix's revealing tests", (_ok.coupled > 0).mean(), len(_ok)),
            ("any line", "as many random tests", _ctl.chance.mean(), len(_ctl)),
            ("the fix's lines", "fix's revealing tests", (_ok.coupled_on_fix > 0).mean(), len(_ok)),
            ("the fix's lines", "as many random tests", _ctl.chance_on_fix.mean(), len(_ctl)),
        ],
        columns=["where", "tests", "share", "faults"],
    )
    _rows["label"] = _rows.share.map(lambda x: f"{x:.0%}")
    _y = alt.Y("where:N", title="coupled to a mutant on", sort=["the fix's lines", "any line"])
    _bars = (
        alt.Chart(_rows)
        .mark_bar()
        .encode(
            x=alt.X("share:Q", title="faults that look coupled", axis=alt.Axis(format="%"), scale=alt.Scale(domain=[0, 1])),
            y=_y,
            yOffset=alt.YOffset("tests:N", sort=["fix's revealing tests", "as many random tests"]),
            color=alt.Color("tests:N", scale=alt.Scale(domain=["fix's revealing tests", "as many random tests"], range=["#4c78a8", "#9e9e9e"]), title=None, legend=alt.Legend(orient="top")),
            tooltip=["where", "tests", alt.Tooltip("share:Q", format=".1%"), "faults"],
        )
    )
    _text = _bars.mark_text(align="left", dx=4, fontSize=11).encode(text="label:N", color=alt.value("black"))
    _ref = alt.Chart(pd.DataFrame({"x": [0.70], "t": ["Google 70%, Just et al. 73%"]}))
    _rule = _ref.mark_rule(strokeDash=[4, 4], color="gray").encode(x="x:Q")
    _rule_text = _ref.mark_text(align="left", dx=4, y=-8, color="gray").encode(x="x:Q", text="t:N")
    _m = _rows.set_index(["where", "tests"]).share
    _chart = (_bars + _text + _rule + _rule_text).properties(
        width=540,
        height=170,
        title=title(
            f"{_m['the fix' + chr(39) + 's lines', 'fix' + chr(39) + 's revealing tests']:.0%} of real bugs link to a mutant on fix lines (random tests: {_m['the fix' + chr(39) + 's lines', 'as many random tests']:.0%})",
            f"{len(_ok)} bugs from {_f.commit.nunique()} commits in 6 repos. Random: 200 equal-sized non-revealing test sets.",
        ),
    )
    _ops = db.sql(f"""
        select op, count(*) filter (where n > 0) "with", count(*) filter (where n > 0 and n = coupled) "only"
        from (
            select coupled, unnest(['ROR', 'COR', 'AOR', 'SDL', 'ERR']) op,
                unnest([coalesce(coupled_operators.ROR, 0), coalesce(coupled_operators.COR, 0), coalesce(coupled_operators.AOR, 0),
                        coalesce(coupled_operators.SDL, 0), coalesce(coupled_operators.ERR, 0)]) n
            from read_json_auto('{_dir}/*.ndjson', union_by_name=true) where status = 'ok'
        ) group by op
    """).df().set_index("op")
    _status = _f.status.value_counts()
    _funnel = ", ".join(f"{n} {st}" for st, n in _status.items())
    _table = _ok.sort_values(["repo", "coupled_on_fix"])[["repo", "commit", "subject", "revealing", "on_fix", "coupled_on_fix", "chance_on_fix", "coupled", "chance"]].copy()
    _table["commit"] = _table.commit.str[:10]
    _table["subject"] = _table.subject.str[:50]
    _table["revealing"] = _table.revealing.map(len)
    _md = "| repo | commit | subject | revealing tests | mutants on fix | coupled on fix | chance on fix | coupled | chance |\n|---|---|---|---:|---:|---:|---:|---:|---:|\n" + "".join(
        f"| {r.repo} | `{r.commit}` | {r.subject} | {r.revealing} | {r.on_fix} | {r.coupled_on_fix} | {r.chance_on_fix:.0%} | {r.coupled} | {r.chance:.0%} |\n" for r in _table.itertuples()
    )
    mo.vstack(
        [
            mo.md("## 10. Are real bugs linked to mutants?"),
            _chart,
            mo.md(rf"""
            - **Protocol:** Pick fix/bug commits touching code and tests in one package. A revealing test passes on the fix and fails before it. A bug is coupled if a mutant on the fixed code is killed only by revealing tests (`adequacy.Matrix.Coupled`, Just et al. FSE 2014).
            - **Candidates:** {_funnel}. Fixes that change an API cannot run new tests on old code.
            - **Why fix lines matter:** On any line, {_m['any line', 'as many random tests']:.0%} of random tests look coupled. On fix lines, random tests almost never do—that gap is the real signal.
            - **Limits:** Small sample ({len(_ok)} bugs); API-changing fixes are excluded; two-package commits count twice.
            - **ERR operator** (`err != nil`, `return err`): {_ops.loc['ERR', 'with']} of {int((_ok.coupled > 0).sum())} coupled bugs include ERR, {_ops.loc['ERR', 'only']} by ERR alone (COR: {_ops.loc['COR', 'with']}/{_ops.loc['COR', 'only']}; ROR: {_ops.loc['ROR', 'with']}/{_ops.loc['ROR', 'only']}). Cheap and clear, though other operators cover the same bugs here.
            """),
            mo.accordion({"Every fault": mo.md(_md)}),
        ]
    )
    return


@app.cell
def _(alt, db, mo, pd, title):
    _dir = mo.notebook_dir() / "labels"
    _l = db.sql(f"""
        select s.key, s.repo, s.operator, s.original, s.mutated, c.label claude, g.label gemini, a.label final, a.how
        from read_json_auto('{_dir}/sample.ndjson') s
        join read_json_auto('{_dir}/claude.ndjson') c using (key)
        join read_json_auto('{_dir}/gemini.ndjson') g using (key)
        join read_json_auto('{_dir}/adjudicated.ndjson') a using (key)
    """).df()
    _cats = ["useful", "equivalent", "unproductive"]
    _n = len(_l)
    _po = (_l.claude == _l.gemini).mean()
    _pe = sum((_l.claude == c).mean() * (_l.gemini == c).mean() for c in _cats)
    _kappa = (_po - _pe) / (1 - _pe)
    _long = pd.concat(
        [_l.assign(rater=r, label=_l[col]) for r, col in [("Claude", "claude"), ("Gemini 4 Neon", "gemini"), ("adjudicated", "final")]]
    )
    _counts = _long.groupby(["rater", "label"], as_index=False).size()
    _counts["share"] = _counts["size"] / _n
    _bars = (
        alt.Chart(_counts)
        .mark_bar()
        .encode(
            x=alt.X("share:Q", title="share of 50 surviving mutants", axis=alt.Axis(format="%"), scale=alt.Scale(domain=[0, 1])),
            y=alt.Y("rater:N", sort=["Claude", "Gemini 4 Neon", "adjudicated"], title=None),
            color=alt.Color("label:N", scale=alt.Scale(domain=_cats, range=["#4c78a8", "#bab0ac", "#f58518"]), title=None, legend=alt.Legend(orient="top")),
            order=alt.Order("label:N", sort="descending"),
            tooltip=["rater", "label", "size", alt.Tooltip("share:Q", format=".0%")],
        )
    )
    _final = _l.final.value_counts()
    _chart = _bars.properties(
        width=540,
        height=130,
        title=title(
            f"{_final.get('useful', 0) / _n:.0%} of survivors are real test gaps; {_final.get('equivalent', 0) / _n:.0%} are equivalent",
            f"50 survivors (5 from shlex, 9 per other repo). Two blind raters agreed on {_po:.0%} (κ = {_kappa:.2f}); diffs settled by running.",
        ),
    )
    _dis = _l[_l.claude != _l.gemini]
    _md = "| mutant | change | Claude | Gemini | settled as | by |\n|---|---|---|---|---|---|\n" + "".join(
        f"| {r.key} | `{r.original[:30]}` → `{(r.mutated or '(deleted)')[:30]}` | {r.claude} | {r.gemini} | **{r.final}** | {r.how.removeprefix('run: ')} |\n" for r in _dis.itertuples()
    )
    _eq = _l[_l.final == "equivalent"]
    mo.vstack(
        [
            mo.md("## 11. What are the surviving mutants?"),
            _chart,
            mo.md(rf"""
            - **Raters:** Claude and Gemini 4 Neon labeled 50 survivors blind (`labels/prompt.md`): *useful* (worth a test), *equivalent* (same behavior for all inputs), *unproductive* (behavior changes, not worth testing).
            - **Disagreements:** All {len(_dis)} were settled by fuzzing original vs mutant (150k–200k inputs each). Gemini was right in all {len(_dis)}.
            - **Cheap rules for the {len(_eq)} equivalents:** Non-negative vs 0 catches 2. `!= ""` → `> ""` catches 4 (72 across the corpus, 0 killed). Deleting `_ = a[15]` catches 1. The remaining 12 need deeper reasoning.
            - **Unproductive ({_final.get('unproductive', 0)}/{_n}):** Rare in pure libraries. Service code with logging/metrics needs arid filtering (§13).
            """),
            mo.accordion({"Disagreements and how they were settled": mo.md(_md)}),
        ]
    )
    return


@app.cell
def _(alt, db, mo, title):
    _ = db.sql("""
        create or replace temp table fx as
        select e.repo, e.pkg, e.file, e.line from_line,
            e.line + len(e.original) - len(replace(e.original, chr(10), '')) to_line, e.compiled,
            coalesce((select bool_or(r.status = 'killed') from ext_results r where r.repo = e.repo and r.mutant_id = e.id), false) killed,
            coalesce((select bool_or(r.status = 'timeout') from ext_results r where r.repo = e.repo and r.mutant_id = e.id), false) timeout
        from ext_mutants e
    """)
    _ = db.sql("""
        create or replace temp table fine as
        select m.repo, m.pkg, m.file, m.line, m.id,
            exists (select 1 from results r where r.repo = m.repo and r.mutant_id = m.id and r.status = 'killed') killed,
            coalesce((select bool_or(not coalesce(s.skipped, false)) from sel_results s where s.repo = m.repo and s.mutant_id = m.id), false) covered
        from mutants m where m.compiled
    """)
    _fn = db.sql("""
        select x.*, (select bool_or(covered) from fine f where f.repo = x.repo and f.pkg = x.pkg and f.file = x.file and f.line between x.from_line and x.to_line) covered
        from fx x
    """).df()
    _k = db.sql("""
        with j as (
            select f.*, x.from_line, x.killed fn_killed, x.timeout fn_timeout, x.compiled fn_compiled,
                (select bool_or(covered) from fine g where g.repo = x.repo and g.pkg = x.pkg and g.file = x.file and g.line between x.from_line and x.to_line) fn_covered
            from fine f left join fx x on x.repo = f.repo and x.pkg = f.pkg and x.file = f.file and f.line between x.from_line and x.to_line
        )
        select case when from_line is null then 'outside any function'
                    when fn_killed then 'body removal fails a test'
                    when fn_timeout or not fn_compiled then 'body removal timed out or did not build'
                    when fn_covered then 'pseudo-tested function'
                    else 'no test reaches the function' end kind,
            count(*) mutants, count(*) filter (where killed) killed, count(*) filter (where not killed) survived
        from j group by all
    """).df()
    _surv = _k.survived.sum()
    _k["share"] = _k.survived / _surv
    _k["label"] = _k.apply(lambda r: f"{r.survived} ({r.share:.0%}); {r.killed} of {r.mutants} killed", axis=1)
    _order = ["body removal fails a test", "pseudo-tested function", "no test reaches the function", "body removal timed out or did not build", "outside any function"]
    _bars = (
        alt.Chart(_k)
        .mark_bar(color="#4c78a8")
        .encode(
            x=alt.X("survived:Q", title="surviving fine-grained mutants"),
            y=alt.Y("kind:N", sort=_order, title="the function they are in"),
            color=alt.condition("datum.kind == 'body removal fails a test'", alt.value("#4c78a8"), alt.value("#bab0ac")),
            tooltip=["kind", "mutants", "killed", "survived", alt.Tooltip("share:Q", format=".0%")],
        )
    )
    _text = _bars.mark_text(align="left", dx=4, fontSize=11).encode(text="label:N", color=alt.value("black"))
    _p = _k.set_index("kind")
    _pt = _p.loc["pseudo-tested function"]
    _alive = (~_fn.killed & ~_fn.timeout & _fn.compiled)
    _chart = (_bars + _text).properties(
        width=480,
        height=190,
        title=title(
            f"{_p.loc['body removal fails a test'].share:.0%} of survivors sit in functions where body removal fails a test",
            f"{len(_fn)} functions, 1 body-removal mutant each. Pseudo-tested: tests run the function but pass without its body.",
        ),
    )
    mo.vstack(
        [
            mo.md("## 12. Does extreme mutation find the same gaps?"),
            _chart,
            mo.md(rf"""
            - **Pseudo-tested:** {int((_alive & _fn.covered.fillna(False)).sum())} of {len(_fn)} functions. Ran in 83 s at `-j 8` ({len(_fn)} mutants vs {_k.mutants.sum():,}).
            - **Not a prefilter:** {_pt.killed} of {_pt.mutants} fine mutants in pseudo-tested functions are still killed (body removal returns zero values tests accept; finer mutants return wrong values or panic). Skipping them saves only {_pt.mutants / _k.mutants.sum():.0%}.
            - **Most gaps are elsewhere:** Most survivors sit in functions where body removal already fails (matches Maton et al. 2024 for Java).
            - **Decision:** Dropped. Fine-grained runs already catch pseudo-tested functions.
            """),
        ]
    )
    return


@app.cell
def _(alt, db, mo, pd, title):
    _dir = mo.notebook_dir() / "labels"
    _l = db.sql(f"""
        select s.key, s.repo, s.arid, c.label claude, g.label gemini, a.label final, a.how
        from read_json_auto('{_dir}/arid-sample.ndjson') s
        join read_json_auto('{_dir}/arid-claude.ndjson') c using (key)
        join read_json_auto('{_dir}/arid-gemini.ndjson') g using (key)
        join read_json_auto('{_dir}/arid-adjudicated.ndjson') a using (key)
    """).df()
    _l["group"] = _l.arid.map(lambda a: "tagged arid" if a else "not tagged")
    _cats = ["useful", "equivalent", "unproductive"]
    _po = (_l.claude == _l.gemini).mean()
    _pe = sum((_l.claude == c).mean() * (_l.gemini == c).mean() for c in _cats)
    _counts = _l.groupby(["group", "final"], as_index=False).size()
    _counts["share"] = _counts["size"] / _counts.groupby("group")["size"].transform("sum")
    _bars = (
        alt.Chart(_counts)
        .mark_bar()
        .encode(
            x=alt.X("share:Q", title="share of the sampled surviving mutants", axis=alt.Axis(format="%"), scale=alt.Scale(domain=[0, 1])),
            y=alt.Y("group:N", sort=["tagged arid", "not tagged"], title=None),
            color=alt.Color("final:N", scale=alt.Scale(domain=_cats, range=["#4c78a8", "#bab0ac", "#f58518"]), title=None, legend=alt.Legend(orient="top")),
            order=alt.Order("final:N", sort="descending"),
            tooltip=["group", "final", "size", alt.Tooltip("share:Q", format=".0%")],
        )
    )
    _t = _l[_l.group == "tagged arid"]
    _u = _l[_l.group == "not tagged"]
    _chart = _bars.properties(
        width=520,
        height=110,
        title=title(
            f"All {int((_t.final == 'unproductive').sum())}/{len(_t)} arid-tagged survivors are noise; {(_u.final == 'useful').mean():.0%} of untagged are useful",
            f"go-retryablehttp and robfig/cron. Two blind raters agreed on {_po:.0%} (κ = {(_po - _pe) / (1 - _pe):.2f}).",
        ),
    )
    _v = db.sql("""
        with agg as (
            select m.repo, m.id, bool_or(r.status = 'killed') k, count(r.status) n
            from ver_mutants m left join ver_results r on r.repo = m.repo and r.mutant_id = m.id group by all
        )
        select m.repo, count(*) mutants, count(*) filter (where m.arid <> '') arid,
            count(*) filter (where m.arid <> '' and a.k) arid_killed,
            count(*) filter (where m.compiled and not coalesce(a.k, false) and a.n > 0) survived,
            count(*) filter (where m.arid <> '' and m.compiled and not coalesce(a.k, false) and a.n > 0) arid_survived
        from ver_mutants m join agg a using (repo, id) group by m.repo order by m.repo
    """).df()
    _flaky = db.sql("select repo, count(*) flaky from ver_results where status = 'flaky' group by repo").df()
    _v = _v.merge(_flaky, on="repo", how="left").fillna({"flaky": 0})
    _rows = "".join(f"| {r.repo} | {r.mutants} | {r.arid} | {r.arid_killed} | {r.survived} | {r.arid_survived} | {int(r.flaky)} |\n" for r in _v.itertuples())
    _md = "| repo | mutants | arid | arid, killed | survived | arid, survived | runs ruled flaky |\n|---|---:|---:|---:|---:|---:|---:|\n" + _rows
    _miss = _u[_u.final == "unproductive"]
    mo.vstack(
        [
            mo.md("## 13. Do arid rules hide only noise?"),
            _chart,
            mo.md(_md),
            mo.md(rf"""
            - **Repos:** 3 service libraries with loggers: `go-retryablehttp`, `robfig/cron`, `hashicorp/yamux`. Slow suites (25–40 s), so we used schemata + selection + fail-fast.
            - **Arid rules (`Mutant.Arid`):** Logging calls (never Fatal/Panic), metrics, tracing, `if`/`switch` blocks doing only those, slice/map capacity hints, and `//allele:ignore`.
            - **Precision / recall:** 100% precision (both blind raters called every tagged survivor noise). {len(_miss)}/{len(_u)} untagged survivors were noise too ({", ".join(_miss.key)}).
            - **Tested logs:** Some tests assert on log lines and kill those arid mutants. Skipping them loses nothing—killed mutants are never reported.
            - **Flake check:** Under `-j 8`, timing tests failed randomly and hid real gaps. Now a kill must fail twice on the mutant and pass on the original (`Runner.Confirm`). Last column counts rejected flaky runs.
            - **Decision:** Skip arid mutants by default (`-arid` includes them).
            """),
        ]
    )
    return


@app.cell
def _(alt, db, mo, pd, title):
    _docs = mo.notebook_dir()
    _wild = "coalesce((to_json(coupled_on_fix_operators)->>'LIT')::int, 0) + coalesce((to_json(coupled_on_fix_operators)->>'CPY')::int, 0) + coalesce((to_json(coupled_on_fix_operators)->>'PNC')::int, 0)"
    _f = db.sql(f"""
        with f as (
            select 'corpus (operators derived here)' grp, *, {_wild} wild from read_json_auto('{_docs}/data-faults-wild/*.ndjson', union_by_name=true) where status = 'ok'
            union all by name
            select 'held out (five other repositories)' grp, *, {_wild} wild from read_json_auto('{_docs}/data-faults-heldout/*.ndjson', union_by_name=true) where status = 'ok'
        )
        select grp, count(*) faults,
            count(*) filter (where coupled_on_fix - wild > 0) old_ops,
            count(*) filter (where coupled_on_fix > 0) with_wild,
            avg(chance_on_fix) filter (where chance_on_fix >= 0) chance
        from f group by grp
    """).df()
    _gained = db.sql(f"""
        select repo, left(subject, 60) subject, to_json(coupled_on_fix_operators) ops
        from read_json_auto('{_docs}/data-faults-wild/*.ndjson', union_by_name=true)
        where status = 'ok' and coupled_on_fix > 0 and coupled_on_fix = {_wild}
    """).df()
    _long = pd.concat(
        [
            _f.assign(ops="old operators", share=_f.old_ops / _f.faults),
            _f.assign(ops="with LIT, CPY, PNC", share=_f.with_wild / _f.faults),
        ]
    )
    _long["label"] = _long.share.map(lambda x: f"{x:.0%}")
    _bars = (
        alt.Chart(_long)
        .mark_bar()
        .encode(
            x=alt.X("share:Q", title="real faults coupled to a mutant on the fix's lines", axis=alt.Axis(format="%"), scale=alt.Scale(domain=[0, 1])),
            y=alt.Y("grp:N", title=None, sort=["corpus (operators derived here)", "held out (five other repositories)"]),
            yOffset=alt.YOffset("ops:N", sort=["old operators", "with LIT, CPY, PNC"]),
            color=alt.Color("ops:N", scale=alt.Scale(domain=["old operators", "with LIT, CPY, PNC"], range=["#9e9e9e", "#4c78a8"]), title=None, legend=alt.Legend(orient="top")),
            tooltip=["grp", "ops", "faults", alt.Tooltip("share:Q", format=".1%")],
        )
    )
    _text = _bars.mark_text(align="left", dx=4, fontSize=11).encode(text="label:N", color=alt.value("black"))
    _h = _f.set_index("grp").loc["held out (five other repositories)"]
    _chart = (_bars + _text).properties(
        width=520,
        height=150,
        title=title(
            f"Fix-derived operators add 0 held-out bugs ({int(_h.old_ops)}/{int(_h.faults)} either way)",
            "LIT (string/regexp literals), CPY (defensive copies), PNC (panic instead of returning error).",
        ),
    )
    _md = "| repo | fix | operator that couples it |\n|---|---|---|\n" + "".join(f"| {r.repo} | {r.subject} | `{r.ops}` |\n" for r in _gained.itertuples())
    mo.vstack(
        [
            mo.md("## 14. Do operators inverted from real fixes help?"),
            _chart,
            mo.md(rf"""
            - **Idea:** Turn uncoupled §10 fixes into operators (Beller et al., ICSE SEIP 2021): LIT (string/regexp), CPY (copies), PNC (panic vs error).
            - **Training set:** Coupled {len(_gained)} more bugs they were designed from—proves nothing.
            - **Held-out set** (`blang/semver`, `cast`, `mapstructure`, `mux`, `strcase`): Coupled 0 new bugs. (`govalidator` timed out at 71 min.)
            - **Cost:** +770 mutants (+15%) on the 6 libraries.
            - **Decision:** Dropped (kept at tag `experiment/wild-caught`).
            """),
            mo.accordion({"Faults coupled only by the new operators": mo.md(_md)}),
        ]
    )
    return


@app.cell
def _(alt, db, mo, pd, title):
    _dir = mo.notebook_dir() / "data-fast-failfast"
    _t = db.sql(f"select * from read_json_auto('{_dir}/timings.ndjson')").df()
    _agg = _t.groupby(["cmd", "repo", "mode"], as_index=False).agg(
        med=("wall_s", "median"),
        lo=("wall_s", "min"),
        hi=("wall_s", "max"),
    )
    _all = _agg[_agg.cmd == "killmatrix_all"].copy()
    _order = _all[_all["mode"] == "before"].sort_values("med", ascending=False).repo.tolist()
    _bars = (
        alt.Chart(_all)
        .mark_bar()
        .encode(
            x=alt.X("med:Q", title="median wall seconds over 3 interleaved runs (-j 8)"),
            y=alt.Y("repo:N", sort=_order, title=None),
            yOffset=alt.YOffset("mode:N", sort=["before", "after"]),
            color=alt.Color(
                "mode:N",
                scale=alt.Scale(domain=["before", "after"], range=["#bab0ac", "#4c78a8"]),
                title=None,
                legend=alt.Legend(orient="top"),
            ),
            tooltip=["repo", "mode", alt.Tooltip("med:Q", format=".2f"), alt.Tooltip("lo:Q", format=".2f"), alt.Tooltip("hi:Q", format=".2f")],
        )
    )
    _err = (
        alt.Chart(_all)
        .mark_rule(color="black", strokeWidth=1.5)
        .encode(
            x="lo:Q",
            x2="hi:Q",
            y=alt.Y("repo:N", sort=_order),
            yOffset=alt.YOffset("mode:N", sort=["before", "after"]),
        )
    )
    _tot = _all.groupby("mode").med.sum()
    _def = _agg[_agg.cmd == "mutant_default"].groupby("mode").med.sum()
    _sem = _all[_all.repo == "semver"].set_index("mode").med
    _chart = (_bars + _err).properties(
        width=540,
        height=240,
        title=title(
            f"Batching + p/t ordering cuts FailFast {_tot['before']:.0f} → {_tot['after']:.0f} s ({_tot['before'] / _tot['after']:.2f}×); semver {_sem['before']:.1f} → {_sem['after']:.1f} s ({_sem['before'] / _sem['after']:.1f}×)",
            "All mutants (-schemata -select -failfast -j 8), 3 rounds (whiskers: min–max). go-version and glob are bound by timeouts.",
        ),
    )
    _v = db.sql(f"""
        with ff as (
            select split_part(parse_path(filename)[-2], '/', 1) repo, id, compiled
            from read_json_auto('{_dir}/*/mutants.ndjson.gz', filename=true)
        ),
        ff_res as (
            select split_part(parse_path(filename)[-2], '/', 1) repo, mutant_id id,
                bool_or(status = 'killed') k, bool_or(status = 'timeout') tmo, bool_or(not coalesce((to_json(r)->>'skipped')::boolean, false)) ran
            from read_json_auto('{_dir}/*/results.ndjson.gz', filename=true) r group by all
        ),
        sel_res as (
            select repo, mutant_id id,
                bool_or(status = 'killed') k, bool_or(status = 'timeout') tmo, bool_or(not coalesce(skipped, false)) ran
            from sel_results group by all
        ),
        paired as (
            select m.repo, m.id,
                case when not m.compiled then 'build error' when f.k then 'killed' when f.tmo then 'timeout' when f.ran then 'survived' else 'not covered' end ff_v,
                case when not m.compiled then 'build error' when s.k then 'killed' when s.tmo then 'timeout' when s.ran then 'survived' else 'not covered' end sel_v
            from ff m left join ff_res f using (repo, id) left join sel_res s using (repo, id)
        )
        select count(*) total,
            count(*) filter (where ff_v <> sel_v) differ,
            count(*) filter (where ff_v = 'timeout' and sel_v in ('killed', 'survived')) false_tmo
        from paired
    """).fetchall()[0]
    mo.vstack(
        [
            mo.md("## 15. Does batching fast tests speed up FailFast?"),
            _chart,
            mo.md(rf"""
            - **Change:** Fast tests (`baseline < 50 ms`) run in batches of up to 32 with `-test.failfast`, saving process startup. Tests are ordered site-reached first, then by `kill_rate / baseline_ms`. Slow tests (`≥ 50 ms`) still run alone.
            - **Speed (`-j 8`, 3 rounds):** All mutants `{_tot['before']:.1f} → {_tot['after']:.1f} s` (`{_tot['before'] / _tot['after']:.2f}×`); default mode `{_def['before']:.1f} → {_def['after']:.1f} s` (`{_def['before'] / _def['after']:.2f}×`). Without timeouts (`semver`), `{_sem['before']:.1f} → {_sem['after']:.1f} s` (`{_sem['before'] / _sem['after']:.1f}×`).
            - **Why `go-version` and `glob` barely moved:** One infinite loop in `go-version` timed out 32 times (`32.6 s` of `33.6 s`). `glob` had 79 timed-out mutants (`165.7 CPU-s`).
            - **Fidelity vs `data-select/`:** {_v[0] - _v[1]:,}/{_v[0]:,} match; **{_v[2]} false timeouts**. All {_v[1]} diffs are in `uuid` (2 clock-dependent, 2 shared package state).
            """),
        ]
    )
    return


@app.cell
def _(alt, db, mo, pd, title):
    _dir = mo.notebook_dir() / "data-speed"
    _t = db.sql(f"select * from read_json_auto('{_dir}/timings.ndjson')").df()
    _agg = _t.groupby(["cmd", "repo", "mode"], as_index=False).agg(
        med=("wall_s", "median"),
        lo=("wall_s", "min"),
        hi=("wall_s", "max"),
    )
    _all = _agg[_agg.cmd == "killmatrix_all"].copy()
    _order = _all[_all["mode"] == "before"].sort_values("med", ascending=False).repo.tolist()
    _bars = (
        alt.Chart(_all)
        .mark_bar()
        .encode(
            x=alt.X("med:Q", title="median wall seconds over 3 interleaved runs (-j 8)"),
            y=alt.Y("repo:N", sort=_order, title=None),
            yOffset=alt.YOffset("mode:N", sort=["before", "after"]),
            color=alt.Color(
                "mode:N",
                scale=alt.Scale(domain=["before", "after"], range=["#bab0ac", "#4c78a8"]),
                title=None,
                legend=alt.Legend(orient="top"),
            ),
            tooltip=["repo", "mode", alt.Tooltip("med:Q", format=".2f"), alt.Tooltip("lo:Q", format=".2f"), alt.Tooltip("hi:Q", format=".2f")],
        )
    )
    _err = (
        alt.Chart(_all)
        .mark_rule(color="black", strokeWidth=1.5)
        .encode(
            x="lo:Q",
            x2="hi:Q",
            y=alt.Y("repo:N", sort=_order),
            yOffset=alt.YOffset("mode:N", sort=["before", "after"]),
        )
    )
    _tot = _all.groupby("mode").med.sum()
    _def = _agg[_agg.cmd == "mutant_default"].groupby("mode").med.sum()
    _gv = _all[_all.repo == "go-version"].set_index("mode").med
    _chart = (_bars + _err).properties(
        width=540,
        height=240,
        title=title(
            f"Parallel baseline + stop-on-timeout cuts wall time {_tot['before']:.0f} → {_tot['after']:.0f} s ({_tot['before'] / _tot['after']:.2f}×); go-version {_gv['before']:.1f} → {_gv['after']:.1f} s ({_gv['before'] / _gv['after']:.0f}×)",
            "All mutants (-j 8, 3 rounds). 18/4,889 shift killed→timeout; 0 false timeouts; 0 flaky baseline tests.",
        ),
    )
    _v = db.sql(f"""
        with sp as (
            select split_part(parse_path(filename)[-2], '/', 1) repo, id, compiled
            from read_json_auto('{_dir}/*/mutants.ndjson.gz', filename=true)
        ),
        sp_res as (
            select split_part(parse_path(filename)[-2], '/', 1) repo, mutant_id id,
                bool_or(status = 'killed') k, bool_or(status = 'timeout') tmo, bool_or(not coalesce((to_json(r)->>'skipped')::boolean, false)) ran
            from read_json_auto('{_dir}/*/results.ndjson.gz', filename=true) r group by all
        ),
        ff_res as (
            select split_part(parse_path(filename)[-2], '/', 1) repo, mutant_id id,
                bool_or(status = 'killed') k, bool_or(status = 'timeout') tmo, bool_or(not coalesce((to_json(r)->>'skipped')::boolean, false)) ran
            from read_json_auto('{mo.notebook_dir()}/data-fast-failfast/*/results.ndjson.gz', filename=true) r group by all
        ),
        paired as (
            select m.repo, m.id,
                case when not m.compiled then 'build error' when s.k then 'killed' when s.tmo then 'timeout' when s.ran then 'survived' else 'not covered' end sp_v,
                case when not m.compiled then 'build error' when f.k then 'killed' when f.tmo then 'timeout' when f.ran then 'survived' else 'not covered' end ff_v
            from sp m left join sp_res s using (repo, id) left join ff_res f using (repo, id)
        )
        select count(*) total,
            count(*) filter (where ff_v = 'killed' and sp_v = 'timeout') k2t,
            count(*) filter (where ff_v = 'survived' and sp_v = 'timeout') false_tmo,
            count(*) filter (where sp_v <> ff_v and not (ff_v = 'killed' and sp_v = 'timeout')) other
        from paired
    """).fetchall()[0]
    _non_ok = db.sql(f"select count(*) from read_json_auto('{_dir}/*/tests.ndjson.gz') where state <> 'ok'").fetchall()[0][0]
    mo.vstack(
        [
            mo.md("## 16. Do parallel baseline and stop-on-timeout help?"),
            _chart,
            mo.md(rf"""
            - **Changes:** (1) Parallel baseline across workers, rechecking any non-OK test alone. (2) `FailFast` stops on the first confirmed timeout (a timed-out mutant cannot survive). (3) Non-schema mutants start first so builds overlap with fast runs.
            - **Speed (`-j 8`, 3 rounds):** All mutants `{_tot['before']:.1f} → {_tot['after']:.1f} s` (`{_tot['before'] / _tot['after']:.2f}×`, `2.5×` faster than pre-§15). Default mode `{_def['before']:.1f} → {_def['after']:.1f} s` (`{_def['before'] / _def['after']:.2f}×`, `2.8×` faster than pre-§15). `go-version`: `{_gv['before']:.1f} → {_gv['after']:.1f} s`.
            - **Fidelity vs §15:** {_v[1]}/{_v[0]:,} mutants stop at `timeout` instead of a later `killed` (both pass `-strict`); **{_v[2]} false timeouts**; {_v[3]} other diff (`uuid`); **{_non_ok} flaky baseline tests**.
            """),
        ]
    )
    return


@app.cell
def _(alt, db, mo, pd, title):
    _docs = mo.notebook_dir()
    # One event per (top-level test, mutant) that failed against the mutant:
    # killed if the failure was confirmed, flaky if Confirm threw it out.
    # Mutant ids stand in for run order, which -j 8 only roughly follows.
    _sim = db.sql(f"""
        with r as (
            select split_part(filename, '/', -2) repo, * from read_json_auto('{_docs}/data-verdicts/*/results.ndjson.gz', filename=true)
            where status in ('killed', 'flaky')
        ), t as (
            select split_part(filename, '/', -2) repo, id, split_part(name, '/', 1) top from read_json_auto('{_docs}/data-verdicts/*/tests.ndjson.gz', filename=true)
        ), e as (
            select r.repo, t.top, r.mutant_id, if(bool_or(status = 'flaky'), 'flaky', 'killed') status
            from r join t on r.repo = t.repo and r.test_id = t.id group by all
        ), s as (
            select *, row_number() over w i, sum((status = 'flaky')::int) over w grp
            from e window w as (partition by repo, top order by mutant_id)
        ), s2 as (
            select *, row_number() over (partition by repo, top, grp order by i) - (grp > 0 and status = 'flaky')::int streak from s
        ), only_flaky as (
            select repo, mutant_id from e group by all having not bool_or(status = 'killed')
        ), n(n) as (values (1), (2), (3), (5), (10), (20)),
        tp as (select n, repo, top, min(i) filter (where status = 'killed' and streak >= n) trust from s2, n group by all),
        x as (select n, s2.*, trust is not null and i > trust trusted from s2 join tp using (repo, top))
        select n, count(*) filter (where not trusted) / count(*) confirmed,
            count(distinct (repo, mutant_id)) filter (where trusted and status = 'flaky' and (repo, mutant_id) in (select (repo, mutant_id) from only_flaky)) wrong,
            (select count(*) from only_flaky) flaky_mutants
        from x group by n order by n
    """).df()
    _long = pd.concat(
        [
            _sim.assign(what="kills still confirmed (cost)", share=_sim.confirmed),
            _sim.assign(what="flaky-only mutants called killed (gaps hidden)", share=_sim.wrong / _sim.flaky_mutants),
        ]
    )
    _chart = (
        alt.Chart(_long)
        .mark_line(point=True)
        .encode(
            x=alt.X("n:O", title="trust a test after this many clean confirmations in a row", axis=alt.Axis(labelAngle=0)),
            y=alt.Y("share:Q", title=None, axis=alt.Axis(format="%"), scale=alt.Scale(domain=[0, 1])),
            color=alt.Color(
                "what:N",
                title=None,
                legend=alt.Legend(orient="top", labelLimit=400),
                scale=alt.Scale(domain=["kills still confirmed (cost)", "flaky-only mutants called killed (gaps hidden)"], range=["#4c78a8", "#e45756"]),
            ),
            tooltip=["n", "what", alt.Tooltip("share:Q", format=".1%")],
        )
        .properties(
            width=520,
            height=220,
            title=title(
                "Skipping confirmation for reliable tests hides gaps before it saves time",
                "Simulated on 3 services' confirmed verdicts (§13).",
            ),
        )
    )
    _10 = _sim.set_index("n").loc[10]
    mo.vstack(
        [
            mo.md("## 17. Can we stop confirming reliable tests?"),
            _chart,
            mo.md(rf"""
            - **Idea:** Confirming a kill adds 2 runs (mutant again, then original). Stop confirming a test after N clean kills in a row.
            - **Result:** Flakiness comes from CPU load, not specific tests. `yamux` timing tests confirm cleanly many times, then flake under load. At N = 10, {_10.confirmed:.0%} of kills still get confirmed, yet {int(_10.wrong)}/{int(_10.flaky_mutants)} flaky-only mutants turn into fake kills.
            - **Decision:** Dropped. Every kill is confirmed.
            """),
        ]
    )
    return


@app.cell
def _(alt, db, mo, title):
    _docs = mo.notebook_dir()
    # One row per build and run of cmd/bench: M, and the errors behind F.
    _m = db.sql(f"""
        with r as (select split_part(split_part(filename, '/', -1), '.', 1) run, * from read_json_auto('{_docs}/data-bench/pad-*.ndjson', filename=true, union_by_name=true))
        select run, bin,
            case bin when 'main' then 1000 when 'pad500' then 500 when 'pad250' then 250 when 'pad100' then 100 when 'pr21' then 100 else 1 end pad_ms,
            max(m) m, max(t) t, sum(hidden) hidden, sum(timeout) timeout
        from r group by all having max(m) is not null order by run, pad_ms
    """).df()
    _lib = _m[~_m.run.isin(["pad-services", "pad-final"])]
    _fin = _m[_m.run == "pad-final"].set_index("bin")
    _chart = (
        alt.Chart(_lib)
        .mark_line(point=True)
        .encode(
            x=alt.X("pad_ms:Q", title="timeout pad over 3× a test's baseline (ms, log)", scale=alt.Scale(type="log", domain=[1, 1000])),
            y=alt.Y("m:Q", title="M", scale=alt.Scale(domain=[1.5, 1.8])),
            color=alt.Color("run:N", title=None, legend=alt.Legend(orient="top")),
            tooltip=["run", "bin", alt.Tooltip("m:Q", format=".3f"), alt.Tooltip("t:Q", format=".1f", title="T (s)"), "hidden", "timeout"],
        )
        .properties(
            width=520,
            height=240,
            title=title(
                "100 ms timeout pad scores best: half the time, 0 hidden gaps",
                "6 libraries, -j 8, worst of 3 reps. pad-stress ran beside 4 busy loops. 1 ms causes false timeouts.",
            ),
        )
    )
    _svc = _m[_m.run == "pad-services"].set_index("bin")
    _q = _lib[_lib.run == "pad-quiet-2"].set_index("bin")
    _base = _lib[(_lib.run == "pad-quiet-1") & (_lib.bin == "main")].iloc[0]
    mo.vstack(
        [
            mo.md("## 18. What timeout pad works best?"),
            _chart,
            mo.md(rf"""
            - **Metric:** `M = D · F · ((876 + 2) / (T + 2))^0.18` from `cmd/bench` (combines detection, fidelity, and speed).
            - **Why pad matters:** Timeout = 3× baseline + pad (was 1 s). Fast tests pay the pad twice on every hang because `Confirm` reruns timeouts.
            - **Result:** Cutting pad from 1 s to 100 ms drops T `{_base.t:.1f} → {_q.loc['pad100'].t:.1f} s` and lifts M `{_base.m:.3f} → {_q.loc['pad100'].m:.3f}` with 0 extra errors (even under load). At 1 ms, {int(_q.loc['pad1'].hidden)} gaps hide behind false timeouts.
            - **Held out (`cron`, `retryablehttp`):** 100 ms hides 0 gaps ({int(_svc.loc['pad100'].hidden)} hidden, {int(_svc.loc['pad100'].timeout)} kills→timeouts vs {int(_svc.loc['main'].timeout)} at 1 s).
            - **Guards:** Batch timeout now sums test baselines (not max), and timeout confirmation gets 3× the limit. Final build: M `{_fin.loc['pr21'].m:.3f}` vs `{_fin.loc['main'].m:.3f}` (T `{_fin.loc['pr21'].t:.1f}` vs `{_fin.loc['main'].t:.1f} s`), 0 extra errors.
            - **Decision:** `allele` uses 100 ms pad; `killmatrix` keeps 1 s for reference runs.
            """),
        ]
    )
    return


@app.cell
def _(alt, db, mo, title):
    _docs = mo.notebook_dir()
    _r = db.sql(f"""
        select bin, repo, median(wall_s) t from read_json_auto('{_docs}/data-bench/infect.ndjson', union_by_name=true)
        where wall_s is not null group by all
    """).df()
    _m = db.sql(f"select bin, m, t, f from read_json_auto('{_docs}/data-bench/infect.ndjson', union_by_name=true) where m is not null").df().set_index("bin")
    _chart = (
        alt.Chart(_r)
        .mark_bar()
        .encode(
            x=alt.X("t:Q", title="median wall time over 5 reps (s)"),
            y=alt.Y("repo:N", title=None),
            yOffset=alt.YOffset("bin:N"),
            color=alt.Color("bin:N", title=None, legend=alt.Legend(orient="top")),
            tooltip=["bin", "repo", alt.Tooltip("t:Q", format=".2f")],
        )
        .properties(
            width=520,
            height=220,
            title=title(
                f"Skipping non-infecting tests: M {_m.loc['main'].m:.3f} → {_m.loc['infect'].m:.3f}",
                "6 libraries, -j 8, 5 reps. Main win is uuid, where survivors skip a 1 s test.",
            ),
        )
    )
    mo.vstack(
        [
            mo.md("## 19. Can we skip tests that never infect a mutant?"),
            _chart,
            mo.md(rf"""
            - **How it works:** A mutant affects a test only if it computes a different value (weak mutation; Howden 1982, Just et al. ISSTA 2014). Baseline helpers already see both operands and log which mutants differ. Tests that never infect a mutant are skipped. Statements, op-assigns, div-by-zero, short-circuited sides, and skipped calls/receives (`if s.Scan()` → `true`) always count as infected.
            - **Result:** T `{_m.loc['main'].t:.1f} → {_m.loc['infect'].t:.1f} s`, F unchanged (`{_m.loc['infect'].f:.4f}`). Gain is modest (+1.1–1.5% M) because most survivors do infect state, they just fail to fail an assertion.
            - **Risk:** A wrong skip (e.g. clock-dependent) can only cause a false gap, never hide a real one.
            - **Decision:** Enabled by default (`Infect`).
            """),
        ]
    )
    return


@app.cell
def _(alt, db, mo, title):
    _docs = mo.notebook_dir()
    _r = db.sql(f"""
        select bin, repo, median(wall_s) t from read_json_auto('{_docs}/data-bench/v2-clone.ndjson', union_by_name=true)
        where wall_s is not null group by all
    """).df()
    _m = db.sql(f"select bin, m, t, f from read_json_auto('{_docs}/data-bench/v2-clone.ndjson', union_by_name=true) where m is not null").df().set_index("bin")
    _chart = (
        alt.Chart(_r)
        .mark_bar()
        .encode(
            x=alt.X("t:Q", title="median wall time over 3 reps (s)"),
            y=alt.Y("repo:N", title=None, sort="-x"),
            yOffset=alt.YOffset("bin:N"),
            color=alt.Color("bin:N", title=None, legend=alt.Legend(orient="top")),
            tooltip=["bin", "repo", alt.Tooltip("t:Q", format=".2f")],
        )
        .properties(
            width=520,
            height=300,
            title=title(
                f"Running unmutated functions as written: M v2 {_m.loc['main'].m:.3f} → {_m.loc['clone'].m:.3f}",
                "10 libraries (M v2), -j 8, 3 reps. Hot loops in sjson and decimal benefit most.",
            ),
        )
    )
    mo.vstack(
        [
            mo.md("## 20. Should unmutated functions run without helpers?"),
            _chart,
            mo.md(rf"""
            - **Problem:** Even with no mutant active, the schema binary ran `sjson`'s fuzz test 4.5× slower. Helper calls block inlining and bounds-check elimination.
            - **Fix:** `Embed` clones each mutated function: `if _mutOff(lo, hi) {{ <original> }} else {{ <with helpers> }}`. Only the active mutant's function (or baseline) enters the helper path. Functions with labels or top-level `:=` reusing params/receivers/results are not cloned.
            - **Result:** T `{_m.loc['main'].t:.1f} → {_m.loc['clone'].t:.1f} s`, F unchanged (`{_m.loc['clone'].f:.4f}`). `sjson`: 45 → 27 s, same verdicts.
            - **Decision:** Enabled in `Embed`.
            """),
        ]
    )
    return


@app.cell
def _(alt, db, mo, title):
    _docs = mo.notebook_dir()
    _j = db.sql(f"""
        select s.split, s.rule, s.j, s.dj, s.dj_se, s.r5, s.reached_line_hit, d.d, d.corpus_survivors
        from read_json_auto('{_docs}/data-selection/selection.ndjson') s
        left join read_json_auto('{_docs}/data-selection/selection-d.ndjson') d using (rule)
    """).df()
    _sp = _j[_j.split != "all"].assign(lo=lambda x: x.dj - 2 * x.dj_se, hi=lambda x: x.dj + 2 * x.dj_se)
    _base = alt.Chart(_sp).encode(
        y=alt.Y("rule:N", title=None, sort=["R2b", "hash", "uninfected first", "fewest infected (>0)", "tiered", "most infected"]),
        yOffset=alt.YOffset("split:N"),
        color=alt.Color("split:N", title=None, legend=alt.Legend(orient="top")),
    )
    _chart = (
        (_base.mark_rule().encode(x=alt.X("lo:Q", title="ΔJ against hash, ± 2 SE (paired over faults)"), x2="hi:Q") + _base.mark_point(filled=True).encode(x="dj:Q", tooltip=["split", "rule", alt.Tooltip("dj:Q", format="+.3f"), alt.Tooltip("dj_se:Q", format=".3f")]))
        .properties(width=520, height=260, title=title(
            "Only R2b gains J on both tuning and held-out bugs",
            "79 real bugs (44 tuning, 35 held-out), one mutant per line, tested with pre-fix suites.",
        ))
    )
    _a = _j[_j.split == "all"].set_index("rule")
    _t = _a[["d", "corpus_survivors", "j", "r5", "reached_line_hit"]].rename(columns={"d": "D (corpus)", "corpus_survivors": "survivors (corpus)", "j": "J", "r5": "R5", "reached_line_hit": "reached-line hit"}).round(3)
    mo.vstack(
        [
            mo.md("## 21. Which mutant should each line keep?"),
            _chart,
            mo.ui.table(_t.reset_index(), selection=None),
            mo.md(rf"""
            - **Question:** Each line keeps one mutant. Should we pick using baseline reach and infection counts instead of a fixed hash?
            - **Data:** `cmd/faults -detail` logged per-mutant fault coupling and old-suite reach/infection across 79 real fixes (`data-faults/detail.ndjson.gz`; scored by `docs/faultsel.py`).
            - **One-sided metrics mislead:** D misses untested code ("fewest infected" raises D to {_a.loc['fewest infected (>0)'].d:.3f} but catches fewer real bugs). R5 rewards hiding survivors ("most infected" beats hash). Hit rate ignores noise ("uninfected first" adds +56% survivors).
            - **Youden's J** weights hits and noisy lines equally (λ = 1), so neither hiding everything nor showing everything wins. At n=79, J resolves differences of ~8–10 points.
            - **R2b** (prefer unreached site → uninfected non-nil `if` → fewest infected > 0) gains J on both splits (+{_a.loc['R2b'].dj:.3f} ± {_a.loc['R2b'].dj_se:.3f} overall), with D {_a.loc['R2b'].d:.3f} vs hash {_a.loc['hash'].d:.3f} and fewer survivors. Caveat: held-out gain alone is within noise.
            - **Decision:** Keep M v2 as the main metric; use R2b as the selection candidate, checked with M v2 and J.
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
    # Kill matrices: about 15 minutes per full run; JOBS=8 by default
    OUT=docs/data docs/corpus.sh
    OUT=docs/data-rerun docs/corpus.sh
    SCHEMATA=1 OUT=docs/data-schemata docs/corpus.sh
    SCHEMATA=1 SELECT=1 OUT=docs/data-select docs/corpus.sh
    JOBS=1 OUT=docs/data-j1 docs/corpus.sh go-version shlex

    # Sections 1-3: simulate the strategies, about 15 seconds
    go run ./cmd/simulate -o docs/data docs/data/*/

    # Section 10: real faults, about 25 minutes over the corpus' history
    for d in shlex go-version glob uuid go-humanize semver; do
      git -C "$CORPUS_CACHE/$d" fetch --unshallow origin   # corpus.sh clones at depth 1
      go run ./cmd/faults -name $d -o docs/data-faults/$d.ndjson "$CORPUS_CACHE/$d"
    done

    # Section 11: sample to label, then label (Gemini: agy -p "$(cat docs/labels/prompt.md)" --model gemini-4-neon-high)
    uv run python docs/labels/sample.py "$CORPUS_CACHE" > docs/labels/sample.ndjson

    # Section 12: extreme mutation, about 90 seconds, with the code as of
    # commit ff8bb8e (removed since; see ROADMAP.md)
    EXTREME=1 OUT=docs/data-extreme docs/corpus.sh

    # Section 13: service verdicts (about an hour), then the arid sample to label
    SCHEMATA=1 SELECT=1 FAILFAST=1 CONFIRM=1 OUT=docs/data-verdicts docs/corpus.sh retryablehttp cron yamux
    uv run python docs/labels/sample_arid.py "$CORPUS_CACHE" > docs/labels/arid-sample.ndjson

    # Section 14: with the code at tag experiment/wild-caught, cmd/faults
    # over the corpus (data-faults-wild/) and over held-out repositories
    # (data-faults-heldout/)

    # Section 21: per-mutant fault detail (about 6 hours over corpus and
    # held-out history), then score the selection rules
    go run ./cmd/faults -name $d -detail detail.ndjson -o rows.ndjson "$CORPUS_CACHE/$d"
    python3 docs/faultsel.py docs/data-faults/detail.ndjson.gz > docs/data-selection/selection.ndjson

    # View everything
    make measurements
    ```
    """)
        }
    )
    return


if __name__ == "__main__":
    app.run()
