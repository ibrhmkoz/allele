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
    for _name, _sub in [("", "data"), ("rerun_", "data-rerun"), ("j1_", "data-j1"), ("sch_", "data-schemata")]:
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

    - **Setup:** M1 Pro 16 GiB on AC power · Go 1.27.3 · `killmatrix -j 8`, so 8 mutants build and run at once; section 6 compares against `-j 1`. Measured on 2026-11-23: two full runs of the same code (`data/`, `data-rerun/`), one with schemata (`data-schemata/`, section 8) and one `-j 1` run of two repositories (`data-j1/`).
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
            - **Still failing:** AOR in constant expressions (`TByte * 1000` → `TByte - 1000` overflows a constant), ERR returning `nil` as a concrete type, an import used only for a type.
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
    JOBS=1 OUT=mutant/docs/data-j1 mutant/docs/corpus.sh go-version shlex

    # Sections 1-3: simulate the strategies, about 15 seconds
    go run ./mutant/cmd/simulate -o mutant/docs/data mutant/docs/data/*/

    # Sections 4-8 query the matrices directly; view everything
    make mutant-measurements
    ```
    """)
        }
    )
    return


if __name__ == "__main__":
    app.run()
