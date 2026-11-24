"""Runs Go mutation tools on targets and records what each reports.

    MUT_BIN=path/to/mutant python3 mutant/docs/compare.py TOOLS TARGETS OUT.ndjson

TOOLS is a comma list of mutant, mutant-all, gremlins, gremlins-all and
gomu. TARGETS has lines "name dir pkg [repo commit]": with repo and commit
the target is a real fault's tree (docs/faulttrees.sh), and the lines its
fix changed are computed. OUT gets one row per (tool, target) as soon as
it is done: seconds, mutants, survivors (survived or not covered), and,
for a fault, the survivors on the files and on the lines the fix touched;
found means at least one on a fix line. Targets already in OUT are
skipped, so a run that stops can go on.

Every tool runs with JOBS workers. On a fault, gomu mutates only the
files the fix touched: its fixed 30 s timeout makes whole packages take
hours, and which faults it finds does not depend on the other files.
"""
import json, os, re, subprocess, sys, time

MUT = os.environ.get("MUT_BIN", "mutant")
GREM = os.path.expanduser("~/go/bin/gremlins")
GOMU = os.path.expanduser("~/go/bin/gomu")
JOBS = os.environ.get("JOBS", "4")


def fix_lines(repo, commit, pkg):
    """Lines of the package's non-test .go files that the fix adds or
    changes, by file base name, numbered as in the fixed tree."""
    out = subprocess.run(["git", "-C", repo, "diff", "-U0", f"{commit}^", commit, "--", pkg],
                         capture_output=True, text=True).stdout
    want = "" if pkg == "." else pkg
    lines, cur = {}, None
    for l in out.splitlines():
        if l.startswith("+++ "):
            cur = None
            p = l[6:] if l.startswith("+++ b/") else None
            if p and p.endswith(".go") and not p.endswith("_test.go") and os.path.dirname(p) == want:
                cur = lines.setdefault(os.path.basename(p), set())
            continue
        m = re.match(r"^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@", l)
        if m and cur is not None:
            start, n = int(m.group(1)), int(m.group(2) or 1)
            if n == 0:  # a deletion: the lines around the gap
                cur.update({start, start + 1})
            cur.update(range(start, start + n))
    return lines


def pattern(pkg):
    return {".": ".", "...": "./..."}.get(pkg, "./" + pkg)


def run_mutant(d, pkg, extra):
    p = subprocess.run([MUT, "-no-cache", "-json", "-j", JOBS, *extra, pattern(pkg)], cwd=d, capture_output=True, text=True)
    rows = []
    for l in p.stdout.splitlines():
        try:
            r = json.loads(l)
        except ValueError:
            continue
        rows.append((os.path.basename(r["file"]), r["line"], {"not covered": "survived"}.get(r["verdict"], r["verdict"])))
    return rows, p.returncode


GREM_ALL = ["--invert-assignments", "--invert-bitwise", "--invert-bwassign", "--invert-logical", "--invert-loopctrl", "--remove-self-assignments"]


def run_gremlins(d, pkg, extra):
    out = os.path.join(d, ".gremlins-out.json")
    target = d if pkg in (".", "...") else os.path.join(d, pkg)
    # Gremlins times each mutant out at a multiple of its coverage run,
    # which a cached test result makes milliseconds long: every mutant
    # would time out. A cold cache gives it the limit it means.
    subprocess.run(["go", "clean", "-testcache"], cwd=d, capture_output=True)
    p = subprocess.run([GREM, "unleash", "--workers", JOBS, "--output", out, *extra, target], cwd=d, capture_output=True, text=True)
    rows = []
    try:
        r = json.load(open(out))
        os.remove(out)
    except (OSError, ValueError):
        return rows, p.returncode or 1
    st = {"KILLED": "killed", "LIVED": "survived", "NOT COVERED": "survived", "TIMED OUT": "timeout", "NOT VIABLE": "build error", "SKIPPED": "skipped"}
    for f in r.get("files", []):
        for m in f.get("mutations", []):
            rows.append((os.path.basename(f["file_name"]), m["line"], st.get(m["status"], m["status"])))
    return rows, p.returncode


def run_gomu(d, pkg, files):
    targets = files or [d if pkg in (".", "...") else os.path.join(d, pkg)]
    p = subprocess.run([GOMU, "run", "--incremental=false", "--workers", JOBS, "--output", "json", "--fail-on-gate=false", *targets],
                       cwd=d, capture_output=True, text=True)
    rows = []
    rep = os.path.join(d, "mutation-report.json")
    try:
        r = json.load(open(rep))
    except (OSError, ValueError):
        return rows, p.returncode or 1
    finally:
        for f in ("mutation-report.json", ".gomu_history.json"):
            try:
                os.remove(os.path.join(d, f))
            except OSError:
                pass
    st = {"KILLED": "killed", "SURVIVED": "survived", "TIMED_OUT": "timeout", "NOT_VIABLE": "build error", "ERROR": "error"}
    for res in r.get("results") or []:
        m = res["mutant"]
        rows.append((os.path.basename(m["filePath"]), m["line"], st.get(res["status"], res["status"].lower())))
    return rows, p.returncode


def main():
    tools, targets, out = sys.argv[1].split(","), sys.argv[2], sys.argv[3]
    done = set()
    if os.path.exists(out):
        for l in open(out):
            r = json.loads(l)
            done.add((r["tool"], r["target"]))
    with open(out, "a") as w:
        for t in open(targets):
            fs = t.split()
            if len(fs) < 3:
                continue
            name, d, pkg = fs[0], fs[1], fs[2]
            fix = fix_lines(fs[3], fs[4], pkg) if len(fs) >= 5 else None
            for tool in tools:
                if (tool, name) in done:
                    continue
                start = time.time()
                if tool == "mutant":
                    rows, code = run_mutant(d, pkg, [])
                elif tool == "mutant-all":
                    rows, code = run_mutant(d, pkg, ["-all"])
                elif tool == "gremlins":
                    rows, code = run_gremlins(d, pkg, [])
                elif tool == "gremlins-all":
                    rows, code = run_gremlins(d, pkg, GREM_ALL)
                elif tool == "gomu":
                    rows, code = run_gomu(d, pkg, [os.path.join(d, pkg, f) for f in fix] if fix else None)
                else:
                    sys.exit(f"unknown tool {tool}")
                secs = time.time() - start
                surv = [(f, l) for f, l, s in rows if s == "survived"]
                row = {"tool": tool, "target": name, "jobs": int(JOBS), "seconds": round(secs, 2), "exit": code, "mutants": len(rows),
                       "survivors": len(surv), "statuses": {s: sum(1 for r in rows if r[2] == s) for s in sorted({r[2] for r in rows})}}
                if fix is not None:
                    row["fix_lines"] = sum(len(v) for v in fix.values())
                    row["fix_files_mutants"] = sum(1 for f, l, s in rows if f in fix)
                    row["fix_files_survivors"] = sum(1 for f, l in surv if f in fix)
                    row["fix_line_survivors"] = sum(1 for f, l in surv if l in fix.get(f, ()))
                    row["found"] = row["fix_line_survivors"] > 0
                w.write(json.dumps(row) + "\n")
                w.flush()
                print(f"{tool:13s} {name:28s} {secs:7.1f}s mutants {len(rows):5d} surv {len(surv):4d}" + (f" found {row['found']}" if fix is not None else ""), flush=True)


main()
