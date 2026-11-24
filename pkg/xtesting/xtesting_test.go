package xtesting_test

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/ibrhmkoz/allele/pkg/xtesting"
)

// spyTB records failures instead of failing the real test.
type spyTB struct {
	testing.TB
	failures []string
}

func (s *spyTB) Helper() {}

func (s *spyTB) Errorf(format string, args ...any) {
	s.failures = append(s.failures, fmt.Sprintf(format, args...))
}

func TestEqualText(t *testing.T) {
	tests := []struct {
		name  string
		got   string
		want  string
		equal bool
	}{
		{
			name:  "same text is equal",
			got:   "Total: 165 TL",
			want:  "Total: 165 TL",
			equal: true,
		},
		{
			name:  "different text is not equal",
			got:   "Total: 132 TL",
			want:  "Total: 165 TL",
			equal: false,
		},
		{
			name: "want is dedented, so it can be indented in the test",
			got:  "Total: 165 TL",
			want: `
				Total: 165 TL
			`,
			equal: true,
		},
		{
			name: "got is not dedented, so an indentation bug still fails",
			got:  "    Total: 165 TL",
			want: `
				Total: 165 TL
			`,
			equal: false,
		},
		{
			name:  "trailing spaces are ignored, they are invisible in a diff",
			got:   "Total: 165 TL   ",
			want:  "Total: 165 TL",
			equal: true,
		},
		{
			name:  "spaces inside a line count",
			got:   "Total: 165  TL",
			want:  "Total: 165 TL",
			equal: false,
		},
		{
			name: "blank lines between lines count",
			got: `- Coffee 120 TL

- Cookie  45 TL`,
			want: `
				- Coffee 120 TL
				- Cookie  45 TL
			`,
			equal: false,
		},
		{
			name:  "text of blank lines only is empty",
			got:   "\n \n\t\n",
			want:  "",
			equal: true,
		},
		{
			name: "want is dedented by its least indented line, wherever it is",
			got:  "Total: 165 TL\n\t- Coffee 120 TL",
			want: `
				Total: 165 TL
					- Coffee 120 TL
			`,
			equal: true,
		},
		{
			name:  "indentation deeper than a short line's text is still removed",
			got:   "A\nB",
			want:  "\n\t\t\tA\n\t\t\tB\n",
			equal: true,
		},
		{
			name:  "tabs then spaces share no indentation",
			got:   "\t\tA\n  B",
			want:  "\t\tA\n  B",
			equal: true,
		},
		{
			name:  "spaces then a tab share no indentation",
			got:   "  A\n\tB",
			want:  "  A\n\tB",
			equal: true,
		},
		{
			name: "a deeper first line does not set the indentation",
			got:  "\t- Coffee 120 TL\nTotal: 165 TL",
			want: `
					- Coffee 120 TL
				Total: 165 TL
			`,
			equal: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spy := &spyTB{}
			xtesting.EqualText(spy, tt.got, tt.want)
			if equal := len(spy.failures) == 0; equal != tt.equal {
				t.Errorf("EqualText(%q, %q): equal = %v, want %v", tt.got, tt.want, equal, tt.equal)
			}
		})
	}
}

type item struct {
	name  string
	price int
}

type cart struct {
	items    []item
	coupon   string
	discount int
}

func (c *cart) Add(name string, price int) { c.items = append(c.items, item{name, price}) }

func (c *cart) Remove(name string) {
	c.items = slices.DeleteFunc(c.items, func(it item) bool { return it.name == name })
}

func (c *cart) ApplyCoupon(code string, percent int) { c.coupon, c.discount = code, percent }

func (c *cart) String() string {
	total := 0
	for _, it := range c.items {
		total += it.price
	}
	total -= total * c.discount / 100

	noun := "items"
	if len(c.items) == 1 {
		noun = "item"
	}

	header := fmt.Sprintf("[OPEN] %d %s", len(c.items), noun)
	if c.coupon != "" {
		header += fmt.Sprintf(" | Coupon: %s (-%d%%)", c.coupon, c.discount)
	}
	header += fmt.Sprintf(" | Total: %d TL", total)

	lines := []string{header}
	for _, it := range c.items {
		lines = append(lines, fmt.Sprintf("- %-8s %3d TL", it.name, it.price))
	}
	return strings.Join(lines, "\n")
}

func TestPrintsReportsMismatch(t *testing.T) {
	spy := &spyTB{}
	xtesting.Prints(spy, &cart{}, "[OPEN] 1 item | Total: 120 TL")
	if len(spy.failures) != 1 {
		t.Errorf("Prints of a cart that differs reported %d failures, want 1", len(spy.failures))
	}
}

func TestPrintsCart(t *testing.T) {
	c := &cart{}
	c.Add("Coffee", 120)
	c.Add("Cookie", 45)
	xtesting.Prints(t, c, `
		[OPEN] 2 items | Total: 165 TL
		- Coffee   120 TL
		- Cookie    45 TL
	`)

	c.ApplyCoupon("SUMMER20", 20)
	xtesting.Prints(t, c, `
		[OPEN] 2 items | Coupon: SUMMER20 (-20%) | Total: 132 TL
		- Coffee   120 TL
		- Cookie    45 TL
	`)

	c.Remove("Cookie")
	xtesting.Prints(t, c, `
		[OPEN] 1 item | Coupon: SUMMER20 (-20%) | Total: 96 TL
		- Coffee   120 TL
	`)
}

func TestEqualTextDiff(t *testing.T) {
	tests := []struct {
		name  string
		color string
		msg   string
	}{
		{
			name:  "plain by default",
			color: "",
			msg: `
				text mismatch (-want +got):
				--- want
				+++ got
				@@ -1,2 +1,2 @@
				 - Coffee   120 TL
				-- Cookie    45 TL
				+- Cookie    50 TL
			`,
		},
		{
			name:  "colored with XTESTING_COLOR=1",
			color: "1",
			msg: `
				text mismatch (-want +got):
				--- want
				+++ got
				@@ -1,2 +1,2 @@
				 - Coffee   120 TL
				<red>-- Cookie    45 TL<reset>
				<green>+- Cookie    50 TL<reset>
			`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XTESTING_COLOR", tt.color)
			spy := &spyTB{}
			got := `- Coffee   120 TL
- Cookie    50 TL`
			xtesting.EqualText(spy, got, `
				- Coffee   120 TL
				- Cookie    45 TL
			`)
			if len(spy.failures) != 1 {
				t.Fatalf("got %d failures, want 1", len(spy.failures))
			}
			xtesting.EqualText(t, showColors(spy.failures[0]), tt.msg)
		})
	}
}

func showColors(s string) string {
	return strings.NewReplacer(
		"\x1b[31m", "<red>",
		"\x1b[32m", "<green>",
		"\x1b[0m", "<reset>",
	).Replace(s)
}

// TestDemo fails on purpose to show what a failing diff looks like.
// Run it with: make demo
func TestDemo(t *testing.T) {
	if os.Getenv("XTESTING_DEMO") != "1" {
		t.Skip("set XTESTING_DEMO=1 to see a failing diff")
	}
	c := &cart{}
	c.Add("Coffee", 120)
	c.Add("Cookie", 50)
	xtesting.Prints(t, c, `
		[OPEN] 2 items | Total: 165 TL
		- Coffee   120 TL
		- Cookie    45 TL
	`)
}
