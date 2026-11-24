// Package xtesting provides test assertions that report readable diffs.
package xtesting

import (
	"fmt"
	"os"
	"strings"
	"testing"

	udiff "github.com/aymanbagabas/go-udiff"
)

// EqualText reports a unified diff when got differs from want. want is
// dedented; got is not, so a real indentation bug still fails. CRLF, trailing
// whitespace and blank lines at the start and end are ignored on both sides.
// Set XTESTING_COLOR=1 for a colored diff.
func EqualText(t testing.TB, got, want string) {
	t.Helper()
	g := normalize(got)
	w := normalize(dedent(want))
	if g == w {
		return
	}
	t.Errorf("text mismatch (-want +got):\n%s", diff(w, g, os.Getenv("XTESTING_COLOR") == "1"))
}

const (
	red   = "\x1b[31m"
	green = "\x1b[32m"
	reset = "\x1b[0m"
)

func diff(want, got string, color bool) string {
	d, err := udiff.ToUnifiedDiff("want", "got", want, udiff.Lines(want, got), udiff.DefaultContextLines)
	if err != nil {
		panic(err) // edits come from udiff.Lines on the same input, so they always apply
	}
	paint := func(code, s string) string {
		if !color {
			return s
		}
		return code + s + reset
	}

	lines := []string{"--- want", "+++ got"}
	for _, h := range d.Hunks {
		var body []string
		from, to := 0, 0
		for _, l := range h.Lines {
			text := strings.TrimSuffix(l.Content, "\n")
			switch l.Kind {
			case udiff.Delete:
				from++
				body = append(body, paint(red, "-"+text))
			case udiff.Insert:
				to++
				body = append(body, paint(green, "+"+text))
			default:
				from++
				to++
				body = append(body, " "+text)
			}
		}
		lines = append(lines, fmt.Sprintf("@@ -%d,%d +%d,%d @@", h.FromLine, from, h.ToLine, to))
		lines = append(lines, body...)
	}
	return strings.Join(lines, "\n") + "\n"
}

// Prints is EqualText on v.String().
func Prints(t testing.TB, v fmt.Stringer, want string) {
	t.Helper()
	EqualText(t, v.String(), want)
}

func normalize(s string) string {
	lines := splitLines(s)
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}

	start, end := 0, len(lines)
	for start < end && lines[start] == "" {
		start++
	}
	for end > start && lines[end-1] == "" {
		end--
	}
	return strings.Join(lines[start:end], "\n")
}

// Blank lines are skipped when computing the shared indentation: the closing
// backtick line and empty lines between paragraphs carry no indentation and
// would otherwise shrink it to "".
func dedent(s string) string {
	lines := splitLines(s)

	var prefix string
	found := false
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		indent := l[:len(l)-len(strings.TrimLeft(l, " \t"))]
		if !found {
			prefix, found = indent, true
			continue
		}
		prefix = commonPrefix(prefix, indent)
	}

	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			lines[i] = ""
			continue
		}
		lines[i] = strings.TrimPrefix(l, prefix)
	}
	return strings.Join(lines, "\n")
}

func splitLines(s string) []string {
	return strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
}

func commonPrefix(a, b string) string {
	n := min(len(a), len(b))
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return a[:i]
}
