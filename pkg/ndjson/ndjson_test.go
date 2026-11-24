package ndjson_test

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ibrhmkoz/allele/pkg/ndjson"
	"github.com/ibrhmkoz/allele/pkg/xtesting"
)

type row struct {
	Name string  `json:"name"`
	Expr string  `json:"expr"`
	N    float64 `json:"n"`
}

func (r row) String() string { return fmt.Sprintf("%s %s %.0f", r.Name, r.Expr, r.N) }

func showRows(rows []row) string {
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintln(&b, r)
	}
	return b.String()
}

func TestEncodeDecode(t *testing.T) {
	in := []row{
		{Name: "a", Expr: "x > 0 && y < 1", N: 1},
		{Name: "b", Expr: strings.Repeat("x", 100_000), N: 2},
	}
	var buf bytes.Buffer
	if err := ndjson.Encode(&buf, in); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), `\u003e`) || strings.Contains(buf.String(), `\u003c`) {
		t.Errorf("Encode escaped HTML characters:\n%s", buf.String()[:120])
	}
	got, err := ndjson.Decode[row](&buf)
	if err != nil {
		t.Fatal(err)
	}
	xtesting.EqualText(t, showRows(got), showRows(in))

	empty, err := ndjson.Decode[row](strings.NewReader("  \n"))
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty input = %v, %v; want empty, nil", empty, err)
	}
	for name, bad := range map[string]string{
		"truncated": "{",
		"not json":  "not json\n",
		"stray":     "}",
	} {
		if _, err := ndjson.Decode[row](strings.NewReader(bad)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if err := ndjson.Encode(&buf, []row{{N: math.NaN()}}); err == nil {
		t.Error("Encode(NaN): no error")
	}
}

func TestWriteReadFile(t *testing.T) {
	dir := t.TempDir()
	in := []row{
		{Name: "first", Expr: "a > b", N: 1},
		{Name: "second", Expr: "c < d", N: 2},
	}

	plain := filepath.Join(dir, "rows.ndjson")
	if err := ndjson.WriteFile(plain, in); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(plain)
	if err != nil {
		t.Fatal(err)
	}
	xtesting.EqualText(t, string(raw), `
		{"name":"first","expr":"a > b","n":1}
		{"name":"second","expr":"c < d","n":2}
	`)
	got, err := ndjson.ReadFile[row](plain)
	if err != nil {
		t.Fatal(err)
	}
	xtesting.EqualText(t, showRows(got), `
		first a > b 1
		second c < d 2
	`)

	gz := filepath.Join(dir, "compressed.ndjson.gz")
	if err := ndjson.WriteFile(gz, in); err != nil {
		t.Fatal(err)
	}
	gzRaw, err := os.ReadFile(gz)
	if err != nil || len(gzRaw) < 2 || gzRaw[0] != 0x1f || gzRaw[1] != 0x8b {
		t.Fatalf("compressed file lacks gzip magic: %x, %v", gzRaw[:min(len(gzRaw), 4)], err)
	}
	// ReadFile reads .gz directly and also falls back from the plain path to path+".gz".
	for _, p := range []string{gz, strings.TrimSuffix(gz, ".gz")} {
		got, err := ndjson.ReadFile[row](p)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", p, err)
		}
		xtesting.EqualText(t, showRows(got), `
			first a > b 1
			second c < d 2
		`)
	}
}

func TestFileErrors(t *testing.T) {
	dir := t.TempDir()
	if err := ndjson.WriteFile(dir, []row{{Name: "x"}}); err == nil {
		t.Error("writing over a directory gave no error")
	}
	nanPath := filepath.Join(dir, "nan.ndjson")
	if err := ndjson.WriteFile(nanPath, []row{{N: math.NaN()}}); err == nil {
		t.Error("writing NaN gave no error")
	}
	if _, err := os.Stat(nanPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("failed WriteFile created %s", nanPath)
	}

	if _, err := ndjson.ReadFile[row](filepath.Join(dir, "missing.ndjson")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing plain file: %v", err)
	}
	missingGZ := filepath.Join(dir, "missing.ndjson.gz")
	if _, err := ndjson.ReadFile[row](missingGZ); !errors.Is(err, fs.ErrNotExist) || strings.Contains(err.Error(), ".gz.gz") {
		t.Errorf("missing .gz file: %v", err)
	}

	badGZ := filepath.Join(dir, "bad.ndjson.gz")
	if err := os.WriteFile(badGZ, []byte("not gzip"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ndjson.ReadFile[row](badGZ); err == nil {
		t.Error("broken gzip gave no error")
	}

	locked := filepath.Join(dir, "locked.ndjson")
	if err := os.WriteFile(locked, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o600) })
	if _, err := ndjson.ReadFile[row](locked); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("unreadable file: %v, want permission error", err)
	}
}

func ExampleEncode() {
	var buf bytes.Buffer
	_ = ndjson.Encode(&buf, []row{
		{Name: "a", Expr: "x > 0", N: 1},
		{Name: "b", Expr: "x < 0", N: 2},
	})
	fmt.Print(buf.String())
	// Output:
	// {"name":"a","expr":"x > 0","n":1}
	// {"name":"b","expr":"x < 0","n":2}
}
