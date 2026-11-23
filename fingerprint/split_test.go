package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/ibrhmkoz/go-kit/xtesting"
)

func splitFile(src string) (shared string, tests map[string]string) {
	h := sha256.New()
	tests = map[string]string{}
	splitTests(h, tests, "p/p_test.go", []byte(src))
	return hex.EncodeToString(h.Sum(nil)), tests
}

// TestSplitTestsAndIsTest checks that Test*, Example*, and Fuzz* functions
// (including names sorting after "TestMain") hash per test, while TestMain and
// receiver methods hash into shared, and reordering test declarations leaves
// both shared and per-test hashes invariant.
func TestSplitTestsAndIsTest(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"Test", true},
		{"TestA", true},
		{"TestZ", true},
		{"Test_1", true},
		{"TestMain", false},
		{"Testify", false},
		{"Example", true},
		{"ExampleFoo", true},
		{"Examplex", false},
		{"Fuzz", true},
		{"FuzzParse", true},
		{"Fuzzy", false},
		{"Helper", false},
	} {
		if got := isTest(tc.name); got != tc.want {
			t.Errorf("isTest(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}

	base := `package p

import "testing"

type stub struct{}

func (stub) TestMethod(t *testing.T) {}

func TestA(t *testing.T) { _ = 1 }

func TestZ(t *testing.T) { _ = 2 }

func ExampleRun() {}

func FuzzRun(f *testing.F) {}
`
	reordered := `package p

import "testing"

type stub struct{}

func (stub) TestMethod(t *testing.T) {}

func FuzzRun(f *testing.F) {}

func TestZ(t *testing.T) { _ = 2 }

func ExampleRun() {}

func TestA(t *testing.T) { _ = 1 }
`
	shared1, tests1 := splitFile(base)
	xtesting.EqualText(t, strings.Join(slices.Sorted(maps.Keys(tests1)), " "), "ExampleRun FuzzRun TestA TestZ")

	shared2, tests2 := splitFile(reordered)
	if shared1 != shared2 || !maps.Equal(tests1, tests2) {
		t.Fatal("reordering test declarations changed the split fingerprint")
	}

	// Editing a method named like a test changes shared only.
	editedMethod := strings.Replace(base, "func (stub) TestMethod(t *testing.T) {}", "func (stub) TestMethod(t *testing.T) { _ = 0 }", 1)
	sharedM, testsM := splitFile(editedMethod)
	if sharedM == shared1 || !maps.Equal(testsM, tests1) {
		t.Fatal("editing a receiver method named Test* should change shared only")
	}

	// Editing TestZ changes only TestZ, not shared or other tests.
	editedZ := strings.Replace(base, "_ = 2", "_ = 3", 1)
	sharedZ, testsZ := splitFile(editedZ)
	if sharedZ != shared1 || testsZ["TestZ"] == tests1["TestZ"] || testsZ["TestA"] != tests1["TestA"] {
		t.Fatal("editing TestZ should change only TestZ's hash")
	}

	// Changing the package clause changes shared only.
	editedPkg := strings.Replace(base, "package p\n", "package p_test\n", 1)
	sharedP, testsP := splitFile(editedPkg)
	if sharedP == shared1 || !maps.Equal(testsP, tests1) {
		t.Fatal("changing package clause should change shared only")
	}
}
