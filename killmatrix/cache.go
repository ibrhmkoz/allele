package killmatrix

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
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
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return
	}
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	dst := cachePath(cacheDir, pkgPath)
	tmp, err := os.CreateTemp(cacheDir, "cache-*.tmp")
	if err != nil {
		return
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return
	}
	_ = os.Rename(name, dst)
}

// snapshotPackage hashes everything that can affect a package's mutation
// verdicts: toolchain, runner options, production files, embed files,
// transitive non-stdlib dependencies, testdata, and test files (splitting
// individual Test*/Example*/Fuzz* functions out so pure test additions can
// reuse previously killed mutants).
func (r Runner) snapshotPackage(pkg *packages.Package, dir string, mutants []Mutant) pkgSnapshot {
	prod := sha256.New()
	_, _ = fmt.Fprintf(prod, "%s\x00%s\x00%s\x00%s\x00%s\x00", cacheFormatVersion, runtime.Version(), runtime.GOOS, runtime.GOARCH, pkg.PkgPath)
	_, _ = fmt.Fprintf(prod, "%v:%v:%v:%v:%d:%g:%d\x00", r.Schemata, r.Select, r.FailFast, r.Confirm, r.BaselineRuns, r.TimeoutFactor, r.TimeoutPad)

	// Production files and //go:embed files of pkg and its non-stdlib deps.
	var files []string
	seen := map[string]bool{}
	addFiles := func(p *packages.Package) {
		for _, f := range p.GoFiles {
			if !seen[f] {
				seen[f] = true
				files = append(files, f)
			}
		}
		for _, f := range p.EmbedFiles {
			if !seen[f] {
				seen[f] = true
				files = append(files, f)
			}
		}
	}
	addFiles(pkg)
	packages.Visit([]*packages.Package{pkg}, nil, func(dep *packages.Package) {
		// Stdlib packages have Module == nil and are pinned by runtime.Version().
		if dep != pkg && dep.Module != nil {
			addFiles(dep)
		}
	})
	// Include testdata/ files if present in the package directory.
	td := filepath.Join(dir, "testdata")
	if info, err := os.Stat(td); err == nil && info.IsDir() {
		_ = filepath.WalkDir(td, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && !seen[p] {
				seen[p] = true
				files = append(files, p)
			}
			return nil
		})
	}
	slices.Sort(files)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		_, _ = fmt.Fprintf(prod, "F:%s:%d\x00", f, len(data))
		prod.Write(data)
	}

	// Parse *_test.go files in dir: individual Test*/Example*/Fuzz* funcs go
	// into testDecls; everything else (imports, helpers, vars, init, TestMain)
	// goes into prod so editing shared test infrastructure invalidates all.
	testDecls := map[string]string{}
	testFiles, _ := filepath.Glob(filepath.Join(dir, "*_test.go"))
	slices.Sort(testFiles)
	for _, tf := range testFiles {
		src, err := os.ReadFile(tf)
		if err != nil {
			continue
		}
		fset := token.NewFileSet()
		af, err := parser.ParseFile(fset, tf, src, 0)
		if err != nil {
			// Unparseable test file: hash raw bytes into prodKey to force rerun.
			_, _ = fmt.Fprintf(prod, "TR:%s:%d\x00", tf, len(src))
			prod.Write(src)
			continue
		}
		_, _ = fmt.Fprintf(prod, "TP:%s:%s\x00", tf, af.Name.Name)
		for _, d := range af.Decls {
			start := fset.Position(d.Pos()).Offset
			end := fset.Position(d.End()).Offset
			chunk := src[start:end]
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && isIndividualTest(fn.Name.Name) {
				sum := sha256.Sum256(chunk)
				testDecls[fn.Name.Name] = hex.EncodeToString(sum[:])
				continue
			}
			_, _ = fmt.Fprintf(prod, "TD:%d\x00", len(chunk))
			prod.Write(chunk)
		}
	}

	prodKey := hex.EncodeToString(prod.Sum(nil))
	full := sha256.New()
	full.Write([]byte(prodKey))
	names := make([]string, 0, len(testDecls))
	for k := range testDecls {
		names = append(names, k)
	}
	slices.Sort(names)
	for _, k := range names {
		_, _ = fmt.Fprintf(full, "T:%s=%s\x00", k, testDecls[k])
	}
	for _, mu := range mutants {
		_, _ = fmt.Fprintf(full, "M:%s\x00", mutantKey(mu))
	}
	return pkgSnapshot{
		fullKey:   hex.EncodeToString(full.Sum(nil)),
		prodKey:   prodKey,
		testDecls: testDecls,
	}
}

func isIndividualTest(name string) bool {
	if name == "TestMain" {
		return false
	}
	return strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Example") || strings.HasPrefix(name, "Fuzz")
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
			if _, ok := killerOf[res.MutantID]; !ok {
				killerOf[res.MutantID] = byTestID[res.TestID]
			}
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
