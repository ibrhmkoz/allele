"""Judges per-line mutant selection rules on real faults (measurements §21).

    python3 mutant/docs/faultsel.py mutant/docs/data-faults/detail.ndjson.gz > mutant/docs/data-selection/selection.ndjson

Each rule keeps one mutant per line. A fault's coupled mutants are those
only its fix's tests kill; the old suite is the tests before the fix. J is
Youden's index per fault, coupled lines whose pick is coupled minus other
lines whose pick survives the old suite, averaged over faults (Gemini's
proposal): it is linear in hits and in noise per line, so neither showing
nothing nor showing everything gains. R5 and the per-line hit rate are kept
to show how each favours one side.
"""
import gzip, json, sys, collections
def fnv32a(s):
    h = 0x811c9dc5
    for b in s.encode():
        h ^= b; h = (h * 0x01000193) & 0xffffffff
    return h
opener = gzip.open if sys.argv[1].endswith('.gz') else open
rows = [json.loads(l) for l in opener(sys.argv[1], 'rt')]
heldout = {'blang-semver','cast','mapstructure','mux','strcase'}
faults = collections.OrderedDict()
for r in rows:
    faults.setdefault((r['repo'], r['commit'], r['pkg']), []).append(r)
from math import comb
def r5(chosen, ms):
    surv = [i for i in chosen if (not ms[i]['killable']) or ms[i]['coupled']]
    c = sum(ms[i]['coupled'] for i in surv)
    if c == 0: return 0.0
    if len(surv) <= 5: return 1.0
    return 1 - comb(len(surv)-c, 5)/comb(len(surv), 5)
def hashpick(idx, ms):
    m = ms[idx[0]]
    return idx[fnv32a(f"{m['file']}:{m['line']}") % len(idx)]
def by_inf(pred, key):
    def pick(idx, ms):
        c = [i for i in idx if pred(ms[i]['infected'])]
        if not c: return hashpick(idx, ms)
        return min(c, key=lambda i: (key(ms[i]['infected']), i))
    return pick
def tiered(idx, ms):
    quiet = [i for i in idx if ms[i]['reached'] > 0 and ms[i]['infected'] == 0]
    if quiet:
        return hashpick(quiet, ms)
    inf = [i for i in idx if ms[i]['infected'] > 0]
    if inf:
        return min(inf, key=lambda i: (ms[i]['infected'], i))
    return hashpick(idx, ms)
def nonnil_cor(m):
    mu = m['mutated']
    return m['operator'] == 'COR' and 'nil' not in m['original'] and (mu in ('true', 'false') or mu.endswith('return true }()') or mu.endswith('return false }()'))
fewest = by_inf(lambda n: n > 0, lambda n: n)
def r2b(idx, ms):
    c = [i for i in idx if ms[i]['reached'] == 0]
    if c: return min(c)
    c = [i for i in idx if ms[i]['reached'] > 0 and ms[i]['infected'] == 0 and nonnil_cor(ms[i])]
    if c: return min(c, key=lambda i: (ms[i]['reached'], i))
    return fewest(idx, ms)
strats = {
  'R2b': r2b,
  'tiered': tiered,
  'hash': hashpick,
  'fewest infected (>0)': by_inf(lambda n: n > 0, lambda n: n),
  'uninfected first': by_inf(lambda n: n >= 0, lambda n: n),
  'most infected': by_inf(lambda n: n >= 0, lambda n: -n),
}
J = {}
out = []
for split in ['tuning', 'heldout', 'all']:
    print(f"== {split}", file=sys.stderr)
    for name, pick in strats.items():
        found = foundfix = total = hit = clines = rhit = rlines = 0
        rsum = 0.0; nsurv = 0; js = []
        for (repo, commit, pkg), ms in faults.items():
            if split != 'all' and (repo in heldout) != (split == 'heldout'): continue
            if not any(m['coupled'] for m in ms): continue
            total += 1
            lines = collections.OrderedDict()
            for i, m in enumerate(ms):
                if m['equivalent'] or m['arid']: continue
                lines.setdefault((m['file'], m['line']), []).append(i)
            chosen = [pick(idx, ms) for idx in lines.values()]
            for idx, c in zip(lines.values(), chosen):
                if any(ms[i]['coupled'] for i in idx):
                    clines += 1
                    hit += ms[c]['coupled']
                    if any(ms[i]['reached'] > 0 for i in idx):
                        rlines += 1
                        rhit += ms[c]['coupled']
            fh = fc = fs = fn = 0
            for idx, c in zip(lines.values(), chosen):
                if any(ms[i]['coupled'] for i in idx):
                    fc += 1; fh += ms[c]['coupled']
                else:
                    fn += 1; fs += (not ms[c]['killable'])
            if fc: js.append(fh/fc - fs/max(fn,1))
            found += any(ms[i]['coupled'] for i in chosen)
            rsum += r5(chosen, ms)
            nsurv += sum(1 for i in chosen if (not ms[i]['killable']) or ms[i]['coupled'])
            foundfix += any(ms[i]['coupled'] and ms[i]['on_fix'] for i in chosen)
        out.append(dict(split=split, rule=name, faults=total, found=found, found_on_fix=foundfix, line_hit=hit/max(clines,1), reached_line_hit=rhit/max(rlines,1), r5=rsum/max(total,1), survivors=nsurv, j=sum(js)/len(js)))
        print(f"  {name:22s} faults found {found}/{total} (on fix lines {foundfix}); coupled lines hit {hit/max(clines,1):.1%} ({clines}); reached {rhit/max(rlines,1):.1%}; R5 {rsum/max(total,1):.3f}; survivors {nsurv}; J {sum(js)/len(js):.3f}", file=sys.stderr)
        J[split, name] = js

import statistics as st
for split in ['tuning', 'heldout', 'all']:
    base = J[split, 'hash']
    for name in strats:
        d = [a-b for a, b in zip(J[split, name], base)]
        se = st.stdev(d)/len(d)**0.5 if len(d) > 1 and any(d) else 0
        print(f"{split:8s} {name:22s} dJ {st.mean(d):+.3f} ± {se:.3f}", file=sys.stderr)
        for o in out:
            if o['split'] == split and o['rule'] == name:
                o['dj'], o['dj_se'] = st.mean(d), se
for o in out:
    print(json.dumps(o))
