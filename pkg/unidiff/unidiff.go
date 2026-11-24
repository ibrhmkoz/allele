// Package unidiff parses unified diff output (git diff -U0, diff -u) into the
// new-file line numbers added or changed, including lines bordering deletions.
package unidiff

import (
	"bufio"
	"bytes"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var hunk = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

type Diff struct {
	files map[string]map[int]bool
}

// Parse reads unified diff output into the new-file lines each file touches.
// Deleted files (+++ /dev/null) are skipped, and pure-deletion hunks mark the
// two lines bordering the gap.
func Parse(out []byte) Diff {
	files := map[string]map[int]bool{}
	var cur map[int]bool
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "+++ ") {
			cur = nil // +++ /dev/null: the file was deleted
			if name, ok := strings.CutPrefix(line, "+++ b/"); ok {
				cur = map[int]bool{}
				files[name] = cur
			}
			continue
		}
		if cur != nil {
			addHunk(cur, line)
		}
	}
	return Diff{files: files}
}

// Lines reads the new-file line numbers touched by hunk headers in out,
// without requiring +++ file headers. Pure-deletion hunks mark the two lines
// bordering the gap.
func Lines(out []byte) map[int]bool {
	lines := map[int]bool{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		addHunk(lines, sc.Text())
	}
	return lines
}

func addHunk(dst map[int]bool, line string) {
	m := hunk.FindStringSubmatch(line)
	if m == nil {
		return
	}
	start, _ := strconv.Atoi(m[1])
	n := 1
	if m[2] != "" {
		n, _ = strconv.Atoi(m[2])
	}
	if n == 0 { //allele:ignore regex (\d+) guarantees n >= 0
		dst[start], dst[start+1] = true, true
		return
	}
	for i := range n {
		dst[start+i] = true
	}
}

func (d Diff) Has(file string, line int) bool {
	return d.files[file][line]
}

// Files returns the touched file paths in sorted order.
func (d Diff) Files() []string {
	return slices.Sorted(maps.Keys(d.files))
}

// Lines returns the touched line numbers of file in sorted order.
func (d Diff) Lines(file string) []int {
	return slices.Sorted(maps.Keys(d.files[file]))
}

func (d Diff) String() string {
	var b strings.Builder
	for _, file := range d.Files() {
		nums := d.Lines(file)
		parts := make([]string, len(nums))
		for i, n := range nums {
			parts[i] = strconv.Itoa(n)
		}
		fmt.Fprintf(&b, "%s: %s\n", file, strings.Join(parts, ", "))
	}
	return b.String()
}
