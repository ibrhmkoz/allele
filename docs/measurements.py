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

    - **Setup:** M1 Pro 16 GiB on AC power · Go 1.27.3 · `killmatrix -j 8`, so 8 mutants build and run at once; section 6 compares against `-j 1`. Measured on 2026-11-23: two full runs of the same code (`data/`, `data-rerun/`), one with schemata (`data-schemata/`, section 8), one with schemata and test selection (`data-select/`, section 9) and one `-j 1` run of two repositories (`data-j1/`).
    - **Corpus:** six repositories at pinned commits (`corpus.sh`): glob, go-humanize, go-version, semver, shlex, uuid. {totals.mutants:,} mutants ({totals.compiled:,} compile, {totals.repaired} of them repaired), {totals.leaves} leaf tests ({totals.excluded} flaky or failing), {totals.results:,} results.
    - **Kill matrix:** every mutant built on its own with `go test -c -vet=off -overlay`, every top-level test run against it alone, a column per leaf subtest read from `-test.v=test2json`. A run times out after 3 × its baseline + 1 s; timeouts ({totals.timeouts:,} results) do not count as kills. Mutants that do not compile are left out of every score.
    - **Score:** the share of dominator mutants killed (Kurtz, Ammann et al., FSE 2016). Strategies are simulated on the matrix, never built: tests are added at random until every mutant the strategy kept is dead, and that test set is scored on all mutants. Each strategy is paired with a control that keeps as many mutants at random. 200 test sets per seed, 30 seeds.
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
            f"{span(summary.killable)} killable mutants per repository come down to {span(summary.dominators)} dominators",
            f"Killing the dominators kills every killable mutant. Log scale. Raw scores {span(summary.raw_score, '{:.2f}')} count the rest again and again.",
        ),
    )
    mo.vstack(
        [
            mo.md("## 1. How much of a raw mutation score is redundancy?"),
            _chart,
            mo.md(r"""
            - **Mechanism:** an `if` with `>` yields 2 COR and 5 ROR mutants; one test usually kills most of them, so they share a kill set and count once.
            - **Limit:** dominators are relative to these tests. Few dominators make a score coarse: with shlex's 2, any score is 0, 0.5 or 1. Section 2 gives each repository's count next to its result.
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
            f"One mutant per line beats random picks in {_opl_wins} of 6 repos; COR alone in {_cor_wins} of 6",
            "Dot: mean over 30 seeds; line: 95% of seeds. Right of 0: tests written against the strategy's mutants kill more dominators. Legend: dominators per repo.",
        ),
    )
    _ties = ", ".join(f"{r.repo} ({r.dominators} dominators, {r.lo:+.3f} to {r.hi:+.3f})" for r in _opl[_opl.lo <= 0].itertuples())
    mo.vstack(
        [
            mo.md("## 2. Which mutant selection beats picking mutants at random?"),
            _chart,
            mo.md(rf"""
            - **One per line** ties random in {_ties}. shlex has the fewest dominators: every adequate test set kills both, so no strategy can differ there. glob is not coarse; its interval straddles 0, a real tie.
            - **COR only:** likely because COR flips whole branches, the faults tests are written to catch (inference, not measured).
            - **Other single operators** mostly lose: exhaustive ROR piles redundant mutants on a few comparisons; AOR and ERR cover few lines.
            - **All but repaired** loses to random in most repos: dropping the mutants that needed a repair costs more dominators than dropping as many at random (section 5).
            - **Seeds** vary only the simulation; a different test suite or corpus could move every dot. Selective vs random mutation in the literature: Zhang et al., ICSE 2010.
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
            f"One mutant per line keeps {_opl.score:.2f} of the score at {_opl.cost:.0%} of the cost",
            "Dot: mean over 6 repos; line: lowest to highest repo. Blue: beats random picks (section 2). Cost includes 8-way build contention.",
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
            f"{span(_main.measured * 100)}% of lines die or survive together; {span(_main.expected * 100)}% would by chance",
            "Lines with ≥ 2 compiled mutants. Expected: pⁿ + (1 − p)ⁿ per line, p the repo's kill rate, if mutants lived or died independently.",
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
            mo.md("## 4. Do the mutants of one line share their fate?"),
            _chart,
            mo.md(_md),
            mo.md(r"""
            - **Source:** Petrović et al., *Does mutation testing improve testing practices?*, ICSE 2021, §IV-D (RQ4): "more than 90% of all lines have a mutant majority fate of 100%", i.e. all of a line's mutants are killed or all survive. Their lines are those touched by 1 502 bug fixes at Google, with arid nodes suppressed and Google's operators; how they count non-compiling and timed-out mutants is not stated.
            - **Same definition here** (first column): killed by the suite or not, non-compiling mutants left out, lines with ≥ 2 mutants. The other columns vary one choice each; *same kill vector* is the strictest (the same tests kill every mutant on the line), *mean majority fate* the most lenient (the share of a line's mutants with its majority's fate).
            - **Mutants agree more than chance where tests are fine-grained** (glob, uuid: about twice the independent rate) and at about chance in go-version, whose 33 top-level tests barely use subtests.
            - **Likely causes of the gap to Google** (inference, not measured): exhaustive ROR puts 5 mutants on every comparison and the boundary one (`>` → `>=`) often survives alone; Google's operators make fewer mutants per line and its lines are bug-fix lines.
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
            f"Keeping orphaned names used leaves {_failed_after} of 4 889 mutants that do not compile, down from 249",
            "_ = x for locals, _ = pkg.Name for imports, in COR, SDL and ERR. Builds also skip vet now.",
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
            mo.md("## 5. Do the repair and the equivalence rule pay off?"),
            _op_chart,
            mo.md(rf"""
            - **Repair vs filter:** {int(summary.repaired.sum())} mutants needed a repair. Dropping them instead would lose dominators: {_gain_txt}. Section 2's *all but repaired* compares dropping them with dropping as many at random.
            - **Still failing:** AOR in constant expressions (`TByte * 1000` → `TByte - 1000` overflows a constant), ERR returning `nil` as a concrete type, an import used only for a type (repaired since with `(*pkg.T)(nil)`, after these runs) or only through a generic function.
            - **Equivalence rule** (a value that cannot be negative compared with 0: `!=` is `>`, `==` is `<=`): {_eq[0]} mutants tagged, {_eq[1]} killed or timed out, so no counterexample. It covers {_eq[0]} of {_surv:,} surviving mutants ({_eq[0] / _surv:.0%}): cheap, but small.
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
            f"Builds take {span(_j8.build_share * 100)}% of the naive runner's time at -j 8",
            "One go test -c per mutant; one process per top-level test per mutant, including timeouts.",
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
            f"Without contention builds are {span(_j1.build_share * 100)}% of the time",
            "go-version and shlex rerun with -j 1.",
        ),
    )
    mo.vstack(
        [
            mo.md("## 6. Where does the naive runner's time go?"),
            _time_chart,
            _j_chart,
            mo.md(rf"""
            - **Timeouts** are under 0.5% of runs but {span(_j1.timeout_share * 100)}% of test-run time at `-j 1`: each waits 3 × baseline + 1 s.
            - **Schemata** (one build per package) would remove the build share; test selection by reach would cut the test runs. Both must reproduce the matrix (section 7 gives the noise floor).
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
            Two full runs of the same code at `-j 8` agree on {_cmp[0] - _cmp[1]:,} of {_cmp[0]:,} cells ({1 - _cmp[1] / _cmp[0]:.3%}). Of the {_cmp[1]} that differ, {_cmp[2]} sit on the timeout limit; the rest come from {_cmp[3]} mutants whose kills change from run to run:
            """),
            mo.md(_md),
            mo.md(rf"""
            - `-j 1` and `-j 8` agree on all but {_j[1]} of {_j[0]:,} cells of go-version and shlex, so parallelism does not change kills there.
            - **For schemata (axis A):** a differential test against this matrix must rerun disagreeing cells before calling them a bug; this many differ between two naive runs alone.
            - Likely causes (inference, not measured): glob's matcher pools its arena with `sync.Pool`, so mutants of its size check depend on GC timing; uuid's version 6 and 7 code reads the clock.
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
            f"Schemata cuts build time {_b.build_s / _s.build_s:.0f}×: {_b.build_s / 60:.0f} to {_s.build_s / 60:.1f} build-minutes",
            f"Faded: a build per mutant; solid: schemata. {_emb:.1%} of mutants embedded; the rest build on their own. Test runs: {_b.run_s / 60:.0f} vs {_s.run_s / 60:.0f} minutes.",
        ),
    )
    _md = "| repo | where | operator | mutant | build per mutant | schemata | cells |\n|---|---|---|---|---|---|---:|\n" + "".join(
        f"| {r.repo} | `{r.loc}` | {r.operator} | `{r.mutated}` | {r.naive} | {r.sch} | {r.cells} |\n" for r in _other.itertuples()
    )
    mo.vstack(
        [
            mo.md("## 8. Does schemata reproduce the matrix, and at what cost?"),
            mo.md(rf"""
            Axis A: the schema binary and a build per mutant agree on {_kinds[0] - _kinds[1]:,} of {_kinds[0]:,} cells ({1 - _kinds[1] / _kinds[0]:.3%}); mutant ids match one to one ({_same_ids} differ in text). Of the {_kinds[1]} cells that differ, {_kinds[2]} sit on the timeout limit and {_kinds[3]} also differ between the two runs of a build per mutant. That leaves {_kinds[4]}; two runs of a build per mutant alone differ on {_kinds[5]} cells (section 7).
            """),
            mo.md(_md) if len(_other) else mo.md("No other cell differs."),
            _chart,
            mo.md(r"""
            - **What the comparison found:** a first schemata run disagreed on semver's `constraints.go:503` and go-humanize's `bytes.go:174`. Find swapped `&&` for `||` as text, which regrouped the tree (`a && b && c` → `a || b && c`, read as `a || (b && c)`); the schema binary, built from the tree, ran the intended mutant. Find now rewrites the node with parentheses, and these runs use it.
            - **Where cells still differ** they fall on the lines section 7 lists as nondeterministic (uuid's clock, glob's pooled arena, go-humanize's `si.go` loop), where any two runs can disagree.
            - **Test runs** get shorter too, likely because no build competes with them for the 8 cores (inference, not measured). Their count does not change: selecting the tests that reach a mutant is the next cut (roadmap 1.3).
            - **Wall clock** of the whole corpus at `-j 8`: about 15 minutes per build to about 6 with schemata (from `corpus.sh`'s `time`).
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
            f"Schemata and test selection run the corpus {_base / _wall['schemata + selection']:.0f}× faster than a build per mutant",
            f"Test processes: {_proc['build per mutant']:,} → {_proc['schemata + selection']:,}. Wall clock from corpus.sh's time; the matrix is the same (below).",
        ),
    )
    mo.vstack(
        [
            mo.md("## 9. Does running only the tests that reach a mutant keep the matrix?"),
            _chart,
            mo.md(rf"""
            - **Selection** skipped {_k[1]:,} of {_k[0]:,} cells ({_k[1] / _k[0]:.0%}): the top-level test never reached the function that holds the mutant in the baseline. {_k[2]} skipped cell differs from the build per mutant, and {_k[2] - _k[3]} of those also differ between its two runs.
            - **By function, not by site:** selecting by site skipped 6 cells that every build per mutant killed. uuid's `getV7Time` bumps `lastV7time` only when two calls share a clock tick; that branch ran in no baseline run but did run against the mutants. A function reaches some site whenever it runs.
            - **Overall** {_k[4]} cells differ from a build per mutant; {_k[5]} are neither on the timeout limit nor among cells its two runs disagree on, all on the nondeterministic lines of section 7 or a mutant built on its own.
            - Mutants outside any function, and those not embedded, always run every test.
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
            f"{_m['the fix' + chr(39) + 's lines', 'fix' + chr(39) + 's revealing tests']:.0%} of real faults are coupled to a mutant on the lines their fix changed; random tests manage {_m['the fix' + chr(39) + 's lines', 'as many random tests']:.0%}",
            f"{len(_ok)} faults (package × fixing commit) from {_f.commit.nunique()} candidate commits in 6 repos. Random: 200 sets of as many non-revealing tests per fault.",
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
            mo.md("## 10. Are real faults coupled to mutants?"),
            _chart,
            mo.md(rf"""
            - **Protocol:** a commit whose message says fix or bug and changes code and tests in one package. Its revealing tests pass on the fix and fail when the changed code files go back to before it. The fault is coupled when a mutant of the fixed code is killed by a revealing test and by no other test (`adequacy.Matrix.Coupled`), as in Just et al. (FSE 2014).
            - **Candidates:** {_funnel}. A fix that changes an API its tests use cannot run its tests on the code before it.
            - **On any line** the measure is weak: a test often kills some mutant no other test kills, whatever the fault, so {_m['any line', 'as many random tests']:.0%} of random test sets look coupled too. **On the fix's lines** random tests almost never do, and that gap is the evidence that mutants stand for real faults.
            - **Limits:** {len(_ok)} faults is a small sample, a few fixes add a broad new test, and a commit touching two packages counts twice. Faults whose revealing tests do not build before the fix are lost.
            - **The Go-specific ERR operator** (`err != nil` → `err == nil`, `return err` → `return nil`), a hypothesis in the roadmap: {_ops.loc['ERR', 'with']} of {int((_ok.coupled > 0).sum())} coupled faults have an ERR mutant among their coupled ones, and {_ops.loc['ERR', 'only']} are coupled through ERR alone (COR: {_ops.loc['COR', 'with']} and {_ops.loc['COR', 'only']}; ROR: {_ops.loc['ROR', 'with']} and {_ops.loc['ROR', 'only']}). ERR is cheap and reads well, but on this corpus another operator always stands for the same fault; section 2 agrees: dropping it is no worse than dropping as many random mutants.
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
            f"{_final.get('useful', 0) / _n:.0%} of surviving mutants point at a real test gap, {_final.get('equivalent', 0) / _n:.0%} are equivalent",
            f"50 survivors, 5 per repo for shlex and 9 for the others. Two blind raters agree on {_po:.0%} (Cohen's κ = {_kappa:.2f}); every disagreement was settled by running the mutant.",
        ),
    )
    _dis = _l[_l.claude != _l.gemini]
    _md = "| mutant | change | Claude | Gemini | settled as | by |\n|---|---|---|---|---|---|\n" + "".join(
        f"| {r.key} | `{r.original[:30]}` → `{(r.mutated or '(deleted)')[:30]}` | {r.claude} | {r.gemini} | **{r.final}** | {r.how.removeprefix('run: ')} |\n" for r in _dis.itertuples()
    )
    _eq = _l[_l.final == "equivalent"]
    mo.vstack(
        [
            mo.md("## 11. What are the mutants that survive? (axis D)"),
            _chart,
            mo.md(rf"""
            - **Raters:** Claude labeled first and wrote its labels down; Gemini 4 Neon (`agy`, high effort) then labeled the same 50 from `labels/prompt.md` without seeing them or the tool's equivalence tags. Labels: *useful* (a maintainer would write a test), *equivalent* (no input changes behavior; performance does not count), *unproductive* (behavior changes, but not worth a test: unreachable or defensive paths, performance hints).
            - **Disagreements, settled by running** the original and the mutant side by side on random inputs (glob: 160 000 pattern/input pairs; go-humanize: 200 333 values; semver: 150 000 versions). Gemini's label held in all {len(_dis)}. Claude had called performance-only changes unproductive against the definition, and missed that a loop's guard breaks on an empty suffix.
            - **Cheap rules for the {len(_eq)} equivalents:** the first rule (unsigned, len, cap vs 0) tags 2. Every string is ≥ "", so `!= ""` → `> ""` is equivalent too: 4 more, and in the whole corpus 72 mutants fit that rule and no test kills one. Deleting `_ = a[15]` on an array is the 7th. The other 12 need reasoning about values (an infinite float's sign is never 0, a loop counter stepping by 3 meets its bound exactly).
            - **Unproductive** is rare here ({_final.get('unproductive', 0)} of {_n}): these libraries log nothing. Arid-node suppression matters more for service code; measuring it needs a corpus with logging, metrics and tracing.
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
            f"{_p.loc['body removal fails a test'].share:.0%} of surviving mutants sit in functions whose body removal a test catches",
            f"{len(_fn)} functions with a body, one extreme mutant each. Pseudo-tested: the tests run the function but pass without its body.",
        ),
    )
    mo.vstack(
        [
            mo.md("## 12. Does removing a whole function's body find the same gaps? (extreme mutation)"),
            _chart,
            mo.md(rf"""
            - **Pseudo-tested functions:** {int((_alive & _fn.covered.fillna(False)).sum())} of {len(_fn)}. Extreme mutation costs one build per function ({len(_fn)} mutants against {_k.mutants.sum():,}); the corpus ran in 83 s at `-j 8`.
            - **Not a prefilter:** the roadmap's idea was to skip the fine-grained mutants of a pseudo-tested function. {_pt.killed} of its {_pt.mutants} are killed by some test: body removal returns the zero value the tests happen to accept (`constraintGreaterThan` returning false, `minLength` returning 0), while a finer mutant returns a wrong non-zero value or panics. Skipping them would hide those kills to save {_pt.mutants / _k.mutants.sum():.0%} of the mutants.
            - **Most gaps are elsewhere:** the bulk of the survivors are in functions whose body removal some test does catch, as Maton, Kapfhammer and McMinn (2024) found for Java; extreme mutation cannot point at them.
            - **Decision:** `mutant` does not prefilter with extreme mutation. The fine-grained run already reports the survivors in pseudo-tested functions.
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
            f"All {int((_t.final == 'unproductive').sum())} of {len(_t)} arid-tagged survivors are unproductive; {(_u.final == 'useful').mean():.0%} of untagged ones are useful",
            f"go-retryablehttp and robfig/cron: every arid-tagged survivor and as many untagged. Two blind raters agree on {_po:.0%} (κ = {(_po - _pe) / (1 - _pe):.2f}).",
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
            mo.md("## 13. Do the arid rules hide only noise?"),
            _chart,
            mo.md(_md),
            mo.md(rf"""
            - **Corpus:** three service libraries that log through a logger type: go-retryablehttp, robfig/cron, hashicorp/yamux. Their tests take 25–40 s, so verdicts come from schemata, selection and failing fast, not a full matrix.
            - **Rules** (`Mutant.Arid`): a call statement that logs (by package or a logger-named type; never Fatal or Panic), records a metric or touches a trace; an if or switch whose branches do only that; a slice capacity or map size hint; a line marked `//mut:ignore`.
            - **Precision:** both raters, blind to the tags, call every tagged survivor unproductive. **Recall:** {len(_miss)} of {len(_u)} untagged survivors are unproductive too ({", ".join(_miss.key)}: an error message's quoted text), so the rules catch most, not all, of the noise.
            - **Some logging is tested:** cron's TestWithVerboseLogger, retryablehttp's ResponseLogHook tests and yamux's captureLogs assert on log lines, and kill those arid mutants. Skipping arid mutants costs nothing there: a killed mutant is never shown.
            - **Flaky kills:** under 8 parallel workers these timing tests fail now and then, which read as kills and hid real survivors. A kill now counts only if the test fails against the mutant twice and passes against the original; when the original fails too, the cell is checked again alone after the run (`Runner.Confirm`). The last column counts the runs that rule threw out.
            - **Decision:** `mutant` skips arid mutants unless `-arid` is given.
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
            f"Operators inverted from fixes add nothing on held-out faults: {int(_h.old_ops)} of {int(_h.faults)} coupled either way",
            "Wild-caught operators: LIT (string and regexp literals), CPY (defensive copies), PNC (panic instead of returning the error).",
        ),
    )
    _md = "| repo | fix | operator that couples it |\n|---|---|---|\n" + "".join(f"| {r.repo} | {r.subject} | `{r.ops}` |\n" for r in _gained.itertuples())
    mo.vstack(
        [
            mo.md("## 14. Do operators inverted from real fixes help? (wild-caught mutants)"),
            _chart,
            mo.md(rf"""
            - **Idea:** invert the corpus' own bug fixes into operators (Beller et al., ICSE SEIP 2021). Of the 12 faults of §10 that no mutant on the fix's lines stood for, 6 were fixes to string or regexp literals, 2 to defensive copies and 1 turned a panic into an error; LIT, CPY and PNC invert them.
            - **On the faults they came from** they couple {len(_gained)} more (table below): expected, and proof of nothing.
            - **On held-out faults** mined from blang/semver, spf13/cast, mitchellh/mapstructure, gorilla/mux and iancoleman/strcase, they couple none the old operators miss. govalidator, the most regexp-heavy candidate, was stopped after 71 minutes unfinished.
            - **Cost:** 770 more mutants on the six libraries, 15% more (counted with the code at the tag).
            - **Decision:** dropped. The code is at tag `experiment/wild-caught`; this section reads the stored runs (`data-faults-wild/`, `data-faults-heldout/`).
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
            f"Batching fast tests + online p/t ordering cuts FailFast wall time {_tot['before']:.0f} → {_tot['after']:.0f} s ({_tot['before'] / _tot['after']:.2f}×); semver {_sem['before']:.1f} → {_sem['after']:.1f} s ({_sem['before'] / _sem['after']:.1f}×)",
            "All mutants (killmatrix -schemata -select -failfast -j 8), 3 interleaved rounds; whiskers show min–max. Remaining time in go-version and glob is dominated by repeated timeouts.",
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
            mo.md("## 15. Does batching fast tests and online ordering speed up FailFast?"),
            _chart,
            mo.md(rf"""
            - **Change:** in `FailFast` mode, fast top-level tests (`baseline_ms < 50 ms`) run in batches of up to 32 via `-test.run "^(T1|…|Tk)$" -test.failfast`, with batch timeout `max(baseline_ms) × factor + pad` (amortizing Go binary startup). Tests are ordered site-reached first, then by online hierarchical Bayes `kill_rate / baseline_ms` (Smith's rule / FaMT). Slow tests (`≥ 50 ms`) still run alone.
            - **Speed (3 interleaved rounds at `-j 8`):** all mutants `{_tot['before']:.1f} → {_tot['after']:.1f} s` (`{_tot['before'] / _tot['after']:.2f}×`); default `mutant ./...` `{_def['before']:.1f} → {_def['after']:.1f} s` (`{_def['before'] / _def['after']:.2f}×`). Where timeouts are absent (`semver`), wall time drops `{_sem['before']:.1f} → {_sem['after']:.1f} s` (`{_sem['before'] / _sem['after']:.1f}×`).
            - **Why `go-version` and `glob` barely move:** in `go-version`, a single infinite-loop mutant times out on 32 tests in a row (`32.6 s` of the `33.6 s` wall time on one worker); in `glob`, 79 timed-out mutants burn `165.7 CPU-s` (`82.5 CPU-s` on 2nd+ timeouts).
            - **Fidelity vs `data-select/`:** {_v[0] - _v[1]:,} of {_v[0]:,} mutants match identically; **{_v[2]} false timeouts** (mutants that timed out in a batch yet passed or died when run alone). The {_v[1]} differing verdicts are all in `uuid` (2 on §7's clock-dependent `TestVersion7Monotonicity`, 2 from package-global state shared across tests in one process, matching `go test ./...`).
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
            f"Parallel baseline + stopping FailFast on timeout cuts wall time {_tot['before']:.0f} → {_tot['after']:.0f} s ({_tot['before'] / _tot['after']:.2f}×); go-version {_gv['before']:.1f} → {_gv['after']:.1f} s ({_gv['before'] / _gv['after']:.0f}×)",
            "All mutants (killmatrix -schemata -select -failfast -j 8), 3 interleaved rounds; whiskers show min–max. 18 of 4 889 mutants change killed→timeout; 0 false timeouts; 0 extra flaky baseline tests after solo recheck.",
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
            mo.md("## 16. Does parallel baseline and stopping FailFast on timeout close the gap?"),
            _chart,
            mo.md(rf"""
            - **Changes:** (1) `baseline` runs top-level tests across `r.Workers` and rechecks any non-OK test alone so latency-sensitive tests (`glob`'s `TestPatternMatch/glob-01`) are not falsely marked flaky under load; (2) `FailFast` stops on the first `Timeout` (confirmed once individually when `Confirm` is set) since a timed-out mutant can never become `survived`; (3) non-schema mutants are dispatched first so individual builds overlap with fast schema runs.
            - **Speed (3 interleaved rounds at `-j 8`):** all mutants `{_tot['before']:.1f} → {_tot['after']:.1f} s` (`{_tot['before'] / _tot['after']:.2f}×`; `2.5×` faster than before §15); default `mutant ./...` `{_def['before']:.1f} → {_def['after']:.1f} s` (`{_def['before'] / _def['after']:.2f}×`; `2.8×` faster than before §15). `go-version` drops `{_gv['before']:.1f} → {_gv['after']:.1f} s` (`{_gv['before'] / _gv['after']:.1f}×`).
            - **Fidelity vs §15:** {_v[1]} of {_v[0]:,} mutants that previously timed out on an earlier test and were killed by a later one now stop at `timeout` (both pass `-strict`); **{_v[2]} false timeouts** (`survived → timeout`); {_v[3]} other diff (`uuid` clock/random); **{_non_ok} flaky/failing baseline tests** across all 6 repositories.
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
                "Trusting a test after clean confirmations hides gaps long before it saves runs",
                "Simulated on the three services' confirmed verdicts (§13); mutant ids stand in for run order.",
            ),
        )
    )
    _10 = _sim.set_index("n").loc[10]
    mo.vstack(
        [
            mo.md("## 17. Can confirmation skip tests that have confirmed cleanly before?"),
            _chart,
            mo.md(rf"""
            - **Idea:** confirming a kill costs two more runs (the mutant again, then the original). Stop confirming a top-level test once it has confirmed N kills in a row, and confirm only tests seen flaky.
            - **Result:** flakiness belongs to the load, not to the test. yamux's timing tests confirm dozens of kills cleanly, then invent kills when the machine gets busy. Hiding at most a handful of gaps takes N = 10, which still confirms {_10.confirmed:.0%} of kills and turns {int(_10.wrong)} of {int(_10.flaky_mutants)} flaky-only mutants into kills.
            - **Decision:** dropped; every kill is confirmed (`cmd/mutant` sets `Confirm`).
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
                "A 100 ms timeout pad scores best: half the time, no gap hidden",
                "Six corpus libraries, -j 8, worst of 3 reps; pad-stress ran beside 4 busy loops. At 1 ms, runs that would finish time out.",
            ),
        )
    )
    _svc = _m[_m.run == "pad-services"].set_index("bin")
    _q = _lib[_lib.run == "pad-quiet-2"].set_index("bin")
    _base = _lib[(_lib.run == "pad-quiet-1") & (_lib.bin == "main")].iloc[0]
    mo.vstack(
        [
            mo.md("## 18. How long should a mutant run before it times out?"),
            _chart,
            mo.md(rf"""
            - **Metric:** `M = D · F · ((876 + 2) / (T + 2))^0.18` from `cmd/bench` (ROADMAP): detection, verdict fidelity and time in one number.
            - **Mechanism:** a run times out after 3 × its test's baseline plus a fixed pad, 1 s by default. Fast tests' baselines already hold the process start, so the pad is almost all of the limit, and every hanging mutant pays it twice, since Confirm reruns a timeout.
            - **Result:** from 1 s to 100 ms, T falls from {_base.t:.1f} s to {_q.loc['pad100'].t:.1f} s and M rises from {_base.m:.3f} to {_q.loc['pad100'].m:.3f}, with no verdict error added, on a quiet machine or beside 4 busy loops. At 1 ms, {int(_q.loc['pad1'].hidden)} gaps hide behind false timeouts.
            - **Held out:** on cron and retryablehttp, whose timing tests are flaky, a 100 ms pad hides no gap ({int(_svc.loc['pad100'].hidden)} hidden, {int(_svc.loc['pad100'].timeout)} kills read as timeouts against {int(_svc.loc['main'].timeout)} at 1 s); their slow tests make the pad irrelevant to time.
            - **Guards (Gemini's review):** a small pad leaves no room for what a limit forgot, so a batch is now limited by the sum of its tests' baselines, not the largest, and the run that confirms a timeout gets thrice the limit. Measured side by side with 1 s, the final build scores {_fin.loc['pr21'].m:.3f} against {_fin.loc['main'].m:.3f} (T {_fin.loc['pr21'].t:.1f} s against {_fin.loc['main'].t:.1f} s), no error added: the guards cost {_fin.loc['pr21'].t - _q.loc['pad100'].t:.1f} s of the gain.
            - **Decision:** `cmd/mutant` sets `TimeoutPad` to 100 ms. `killmatrix` keeps 1 s for the reference matrices.
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
                f"Skipping tests that never infect a mutant: M {_m.loc['main'].m:.3f} → {_m.loc['infect'].m:.3f}",
                "Six corpus libraries, -j 8, 5 interleaved reps; the time goes down in uuid, whose survivors meet a 1 s test.",
            ),
        )
    )
    mo.vstack(
        [
            mo.md("## 19. Do tests that never infect a mutant need to run against it?"),
            _chart,
            mo.md(rf"""
            - **Mechanism:** a mutant changes what a test sees only if, at its site, the mutated expression computes a different value (weak mutation: Howden 1982; Just et al., ISSTA 2014). In the baseline the schema helpers already hold both operands, so they record which mutants would have differed; a test that ran a site and never infected its mutant is not run against it and counts as survived. Statements, op-assignments, a division by zero on either side, and the side of && or || the original never evaluates count as infected; so does a mutant that skips an expression with a call or a receive in it (`if s.Scan()` → `if true`), since skipping it changes more than a value (Gemini's review).
            - **Result:** T {_m.loc['main'].t:.1f} s → {_m.loc['infect'].t:.1f} s, F unchanged at {_m.loc['infect'].f:.4f}. Four runs agreed (+1.1% to +1.5% in M), the last after the review made the filter sound; the gain is small because most survivors here are infected and fail to propagate, as uuid's state changes the slow test never checks.
            - **Risk:** a skip is wrong only if a test infects a mutant in its run but not in the baseline, as clock values can; that misses a kill (a false gap), never hides one.
            - **Decision:** `cmd/mutant` sets `Infect`.
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
                "Ten corpus libraries (M v2), -j 8, 3 interleaved reps. sjson's fuzz test and decimal's suite spend their time in hot loops.",
            ),
        )
    )
    mo.vstack(
        [
            mo.md("## 20. Does the schema binary need to run every function through its helpers?"),
            _chart,
            mo.md(rf"""
            - **Finding:** the schema binary ran sjson's fuzz test 4.5 times slower than the plain binary with no mutant of the package active: every site calls a helper, and a function that calls helpers is no longer inlined, nor are its bounds checks proven. Every survivor runs its tests on that binary.
            - **Mechanism** (Gemini's design): Embed clones each function that holds a site. `if _mutOff(lo, hi) {{ <body as written> }} else {{ <body with helpers> }}`: only the function holding the active mutant, or a baseline recording reach, takes the mutated copy. The original keeps its lines; `/*line*/` directives give the copy the same positions. Bodies with labels, or whose top level := reuses a receiver, parameter or result, are not cloned: the extra block would change what they mean (Gemini found the parameter case: a closure made before the := saw the old value).
            - **Result:** T {_m.loc['main'].t:.1f} s → {_m.loc['clone'].t:.1f} s, F unchanged at {_m.loc['clone'].f:.4f}; sjson 45 → 27 s with the same verdicts.
            - **Decision:** Embed clones.
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
            "Only R2b gains J on both the tuning and the held-out faults",
            "79 real faults (44 tuning, 35 held-out), one mutant per line, old suite = tests before the fix.",
        ))
    )
    _a = _j[_j.split == "all"].set_index("rule")
    _t = _a[["d", "corpus_survivors", "j", "r5", "reached_line_hit"]].rename(columns={"d": "D (corpus)", "corpus_survivors": "survivors (corpus)", "j": "J", "r5": "R5", "reached_line_hit": "reached-line hit"}).round(3)
    mo.vstack(
        [
            mo.md("## 21. Which mutant should each line keep? Judging selection by real faults"),
            _chart,
            mo.ui.table(_t.reset_index(), selection=None),
            mo.md(rf"""
            - **Question:** mutant keeps one mutant per line. The old suite's reach and infection counts say which of a line's mutants no test touches. Should the pick use them?
            - **Data:** `cmd/faults -detail` records, for each mutant of 79 real fault fixes, whether only the fix's new tests kill it (coupled to the fault) and how many old tests reach and infect it (`data-faults/detail.ndjson.gz`; `docs/faultsel.py` scores the rules).
            - **Every one-sided measure picks wrong.** D, simulated with the existing tests, cannot see gaps no test reaches into, where real faults hide: "fewest infected" raises D to {_a.loc['fewest infected (>0)'].d:.3f} but finds fewer coupled lines. R5 (chance that reading 5 survivors finds a coupled one) rewards showing fewer survivors: the control rule "most infected" beats hash. The per-line hit rate ignores noise: "uninfected first" wins it at +56% survivors.
            - **J** (Youden's index, Gemini's proposal) charges a hit and a noisy line on the same per-line scale, so neither showing nothing nor showing everything gains. Decision theory says any single number fixes some exchange rate λ between them: "bias-free" means stating λ (here 1). The control loses to hash on both splits; at n=79, the smallest difference J can resolve is about 8–10 points.
            - **R2b** (prefer a mutant whose site no old test reaches, then a non-nil `if` → true/false no old test infects, then the fewest infected above 0) gains J on both splits (+{_a.loc['R2b'].dj:.3f} ± {_a.loc['R2b'].dj_se:.3f} over all), with D {_a.loc['R2b'].d:.3f} against hash's {_a.loc['hash'].d:.3f} and fewer corpus survivors. Caveat: it was designed on these faults; the held-out gain alone is inside the noise.
            - **Decision:** not adopted as a metric (M stays v2; R5 rejected). R2b is the candidate for selection, to be measured by M v2 and checked with J. D per rule came from a scratch tool that simulated each rule's picks on the reference matrices with the corpus' infection counts.
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
    OUT=mutant/docs/data mutant/docs/corpus.sh
    OUT=mutant/docs/data-rerun mutant/docs/corpus.sh
    SCHEMATA=1 OUT=mutant/docs/data-schemata mutant/docs/corpus.sh
    SCHEMATA=1 SELECT=1 OUT=mutant/docs/data-select mutant/docs/corpus.sh
    JOBS=1 OUT=mutant/docs/data-j1 mutant/docs/corpus.sh go-version shlex

    # Sections 1-3: simulate the strategies, about 15 seconds
    go run ./mutant/cmd/simulate -o mutant/docs/data mutant/docs/data/*/

    # Section 10: real faults, about 25 minutes over the corpus' history
    for d in shlex go-version glob uuid go-humanize semver; do
      git -C "$CORPUS_CACHE/$d" fetch --unshallow origin   # corpus.sh clones at depth 1
      go run ./mutant/cmd/faults -name $d -o mutant/docs/data-faults/$d.ndjson "$CORPUS_CACHE/$d"
    done

    # Section 11: sample to label, then label (Gemini: agy -p "$(cat mutant/docs/labels/prompt.md)" --model gemini-4-neon-high)
    uv run python mutant/docs/labels/sample.py "$CORPUS_CACHE" > mutant/docs/labels/sample.ndjson

    # Section 12: extreme mutation, about 90 seconds, with the code as of
    # commit ff8bb8e (removed since; see ROADMAP.md)
    EXTREME=1 OUT=mutant/docs/data-extreme mutant/docs/corpus.sh

    # Section 13: service verdicts (about an hour), then the arid sample to label
    SCHEMATA=1 SELECT=1 FAILFAST=1 CONFIRM=1 OUT=mutant/docs/data-verdicts mutant/docs/corpus.sh retryablehttp cron yamux
    uv run python mutant/docs/labels/sample_arid.py "$CORPUS_CACHE" > mutant/docs/labels/arid-sample.ndjson

    # Section 14: with the code at tag experiment/wild-caught, cmd/faults
    # over the corpus (data-faults-wild/) and over held-out repositories
    # (data-faults-heldout/)

    # Section 21: per-mutant fault detail (about 6 hours over corpus and
    # held-out history), then score the selection rules
    go run ./mutant/cmd/faults -name $d -detail detail.ndjson -o rows.ndjson "$CORPUS_CACHE/$d"
    python3 mutant/docs/faultsel.py mutant/docs/data-faults/detail.ndjson.gz > mutant/docs/data-selection/selection.ndjson

    # Section 17 simulates on data-verdicts/; sections 4-17 query the data
    # directly; view everything
    make mutant-measurements
    ```
    """)
        }
    )
    return


if __name__ == "__main__":
    app.run()
