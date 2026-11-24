package killmatrix

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/ibrhmkoz/allele/pkg/fingerprint"
)

const cacheFormatVersion = "v1"

// cacheEntry stores the last FailFast verdict snapshot of one package.
type cacheEntry struct {
	FullKey   string            `json:"full_key"`
	ProdKey   string            `json:"prod_key"`
	TestDecls map[string]string `json:"test_decls"`
	Mutants   []cachedMutant    `json:"mutants"`
}

type cachedMutant struct {
	Key       string  `json:"key"`
	Verdict   Verdict `json:"verdict"`
	KillerTop string  `json:"killer_top,omitempty"`
}

type pkgSnapshot struct {
	fullKey   string
	prodKey   string
	testDecls map[string]string
}

func mutantKey(mu Mutant) string {
	return fmt.Sprintf("%s:%d:%d:%s:%s->%s", mu.File, mu.Line, mu.Col, mu.Operator, mu.Original, mu.Mutated)
}

func cachePath(cacheDir, pkgPath string) string {
	sum := sha256.Sum256([]byte(pkgPath))
	return filepath.Join(cacheDir, hex.EncodeToString(sum[:16])+".json")
}

func loadCache(cacheDir, pkgPath string) (cacheEntry, bool) {
	data, err := os.ReadFile(cachePath(cacheDir, pkgPath))
	if err != nil {
		return cacheEntry{}, false
	}
	var e cacheEntry
	if err := json.Unmarshal(data, &e); err != nil {
		return cacheEntry{}, false
	}
	return e, true
}

func saveCache(cacheDir, pkgPath string, e cacheEntry) {
	// Best-effort persistence: filesystem errors drop the cache write rather
	// than failing the run.
	if err := os.MkdirAll(cacheDir, 0o700); err != nil { //allele:ignore
		return
	}
	data, err := json.Marshal(e)
	if err != nil { //allele:ignore
		return
	}
	tmp, err := os.CreateTemp(cacheDir, "cache-*.tmp")
	if err != nil { //allele:ignore
		return
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil { //allele:ignore
		_ = os.Remove(name) //allele:ignore
		return
	}
	_ = os.Rename(name, cachePath(cacheDir, pkgPath))
}

// snapshot keys a package's cache entry: its fingerprint and the runner
// options that change verdicts, then its tests and the mutants chosen.
func (r Runner) snapshot(p fingerprint.Print, mutants []Mutant) pkgSnapshot {
	prod := sha256.New()
	// Whether Pick runs changes which mutants a run keeps: with it the
	// entry holds one per line, without it every candidate.
	_, _ = fmt.Fprintf(prod, "%s\x00%s\x00%v:%v:%v:%v:%v:%d:%g:%d:%v\x00",
		cacheFormatVersion, p.Shared,
		r.Schemata, r.Select, r.Infect, r.FailFast, r.Confirm, r.BaselineRuns, r.TimeoutFactor, r.TimeoutPad, r.Pick != nil) //allele:ignore any boolean that differs keeps the two apart
	prodKey := hex.EncodeToString(prod.Sum(nil))
	full := sha256.New()
	_, _ = full.Write([]byte(prodKey))
	for _, name := range slices.Sorted(maps.Keys(p.Tests)) {
		_, _ = fmt.Fprintf(full, "T:%s=%s\x00", name, p.Tests[name])
	}
	for _, mu := range mutants {
		_, _ = fmt.Fprintf(full, "M:%s\x00", mutantKey(mu))
	}
	return pkgSnapshot{fullKey: hex.EncodeToString(full.Sum(nil)), prodKey: prodKey, testDecls: p.Tests}
}

func cachedResult(id int, v Verdict) (compiled bool, res []Result) {
	switch v {
	case VerdictNoBuild:
		return false, nil
	case VerdictNotCovered:
		return true, nil
	case VerdictKilled:
		return true, []Result{{MutantID: id, Status: Killed}}
	case VerdictTimeout:
		return true, []Result{{MutantID: id, Status: Timeout}}
	default:
		return true, []Result{{MutantID: id, Status: Survived}}
	}
}

// buildCacheEntry constructs the cacheEntry to persist after a package run
// finishes, merging any Level-2 reused kills with freshly computed verdicts.
func buildCacheEntry(snap pkgSnapshot, m *Matrix, reusedKillers map[int]string) cacheEntry {
	byTestID := map[int]string{}
	for _, t := range m.Tests {
		byTestID[t.ID] = topOf(t.Name)
	}
	killerOf := map[int]string{}
	maps.Copy(killerOf, reusedKillers)
	for _, res := range m.Results {
		if res.Status == Killed && res.TestID > 0 {
			killerOf[res.MutantID] = byTestID[res.TestID]
		}
	}
	verdicts := m.Verdicts()
	out := make([]cachedMutant, len(m.Mutants))
	for i, mu := range m.Mutants {
		v := verdicts[mu.ID]
		out[i] = cachedMutant{
			Key:       mutantKey(mu),
			Verdict:   v,
			KillerTop: killerOf[mu.ID],
		}
	}
	return cacheEntry{
		FullKey:   snap.fullKey,
		ProdKey:   snap.prodKey,
		TestDecls: snap.testDecls,
		Mutants:   out,
	}
}
