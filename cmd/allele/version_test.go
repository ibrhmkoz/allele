package main

import (
	"fmt"
	"runtime/debug"
	"testing"

	"github.com/ibrhmkoz/allele/internal/xtesting"
)

func TestVersionOf(t *testing.T) {
	installed := &debug.BuildInfo{Main: debug.Module{Version: "v0.1.1"}}
	local := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}
	xtesting.EqualText(t, fmt.Sprintln(
		versionOf("0.2.0", installed, true),
		versionOf("", installed, true),
		versionOf("", local, true),
		versionOf("", &debug.BuildInfo{}, true),
		versionOf("", nil, false),
	), "0.2.0 v0.1.1 devel devel devel\n")
}
