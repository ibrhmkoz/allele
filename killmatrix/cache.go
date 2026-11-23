package killmatrix

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

const cacheFormatVersion = "v1"

// loadDepMeta loads package and test-package dependency metadata without
// parsing or type-checking test files, returning all package variants
// (production + test) keyed by their package path.
func loadDepMeta(ctx context.Context, dir string, patterns []string) (map[string][]*packages.Package, error) {
	cfg := &packages.Config{
		Context: ctx,
		Dir:     dir,
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedImports | packages.NeedDeps | packages.NeedEmbedFiles | packages.NeedForTest | packages.NeedModule,
		Tests:   true,
	}
	loaded, err := packages.Load(cfg, patterns...)
	if err != nil { //mut:ignore
		return nil, err //mut:ignore
	}
	byPkg := map[string][]*packages.Package{}
	for _, p := range loaded {
		k := cmp.Or(p.ForTest, p.PkgPath)
		byPkg[k] = append(byPkg[k], p)
	}
	return byPkg, nil
}

// goToolchain returns "GOVERSION\nGOOS\nGOARCH" for the go command in dir,
// respecting GOTOOLCHAIN and go.mod rather than the mutant binary's runtime.
func goToolchain(ctx context.Context, dir string) string {
	cmd := exec.CommandContext(ctx, "go", "env", "GOVERSION", "GOOS", "GOARCH")
	cmd.Dir = dir //mut:ignore
	out, err := cmd.Output()
	if err != nil { //mut:ignore
		return ""
	}
	return strings.TrimSpace(string(out))
}

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
	if err := os.MkdirAll(cacheDir, 0o700); err != nil { //mut:ignore
		return
	}
	data, err := json.Marshal(e)
	if err != nil { //mut:ignore
		return
	}
	tmp, err := os.CreateTemp(cacheDir, "cache-*.tmp")
	if err != nil { //mut:ignore
		return
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil { //mut:ignore
		_ = os.Remove(name) //mut:ignore
		return
	}
	_ = os.Rename(name, cachePath(cacheDir, pkgPath))
}

// snapshotPackage hashes everything that can affect a package's mutation
// verdicts: toolchain, runner options, production files, embed files,
// transitive non-stdlib dependencies (of both production and test packages),
// testdata, and test files (splitting individual Test*/Example*/Fuzz*
// functions out so pure test additions can reuse previously killed mutants).
func (r Runner) snapshotPackage(pkg *packages.Package, depRoots []*packages.Package, toolchain, dir string, mutants []Mutant) pkgSnapshot {
	prod := sha256.New()
	_, _ = fmt.Fprintf(prod, "%s\x00%s\x00%s\x00%v:%v:%v:%v:%d:%g:%d\x00",
		cacheFormatVersion, toolchain, pkg.PkgPath,
		r.Schemata, r.Select, r.FailFast, r.Confirm, r.BaselineRuns, r.TimeoutFactor, r.TimeoutPad)

	var files []string
	packages.Visit(depRoots, nil, func(dep *packages.Package) {
		// Skip test variants of pkg itself (whose *_test.go are hashed
		// per-declaration below) and stdlib (pinned by GOVERSION).
		if dep.ForTest == pkg.PkgPath || dep.Module == nil {
			return
		}
		files = append(files, dep.GoFiles...)
		files = append(files, dep.EmbedFiles...)
	})
	_ = filepath.WalkDir(filepath.Join(dir, "testdata"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	// Sorting and deduping keep the hash deterministic and avoid reading a file twice.
	slices.Sort(files)            //mut:ignore
	files = slices.Compact(files) //mut:ignore
	for _, f := range files {
		hashFile(prod, f)
	}

	// Parse *_test.go files in dir: individual Test*/Example*/Fuzz* funcs go
	// into testDecls; everything else (imports, helpers, vars, init, TestMain)
	// goes into prod so editing shared test infrastructure invalidates all.
	testDecls := map[string]string{}
	testFiles, _ := filepath.Glob(filepath.Join(dir, "*_test.go"))
	slices.Sort(testFiles) //mut:ignore
	for _, tf := range testFiles {
		src, err := os.ReadFile(tf)
		if err != nil { //mut:ignore
			continue
		}
		fset := token.NewFileSet()
		af, err := parser.ParseFile(fset, tf, src, 0)
		if err != nil { //mut:ignore
			hashBytes(prod, tf, src) //mut:ignore
			continue
		}
		for _, d := range af.Decls {
			chunk := src[fset.Position(d.Pos()).Offset:fset.Position(d.End()).Offset]
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && isIndividualTest(fn.Name.Name) {
				sum := sha256.Sum256(chunk)
				testDecls[fn.Name.Name] = hex.EncodeToString(sum[:])
				continue
			}
			hashBytes(prod, tf, chunk)
		}
	}

	prodKey := hex.EncodeToString(prod.Sum(nil))
	full := sha256.New()
	full.Write([]byte(prodKey))
	names := make([]string, 0, len(testDecls))
	for k := range testDecls {
		names = append(names, k)
	}
	slices.Sort(names) //mut:ignore
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

func hashFile(w io.Writer, path string) {
	data, err := os.ReadFile(path)
	if err != nil { //mut:ignore
		return
	}
	hashBytes(w, path, data)
}

func hashBytes(w io.Writer, label string, data []byte) {
	// Length-prefix framing prevents boundary collisions across adjacent chunks.
	_, _ = fmt.Fprintf(w, "%s:%d\x00", label, len(data)) //mut:ignore
	_, _ = w.Write(data)
}

func isIndividualTest(name string) bool {
	// TestMain wraps the entire test binary, so editing it must invalidate all kills.
	if name == "TestMain" { //mut:ignore
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
