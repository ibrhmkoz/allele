// Command nextversion prints the tag the next release takes, or nothing
// when the commits since the last tag need none.
//
// Each commit since the last v* tag asks for a level: its "Release:"
// trailer (major, minor, patch or none) if it has one, else patch when it
// touches more than docs/ and Markdown, else none. The release bumps by
// the highest level asked for.
//
//	go run ./internal/nextversion
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
)

type commit struct {
	release string // the Release trailer, if any
	files   []string
}

const (
	none = iota
	patch
	minor
	major
)

func (c commit) level() int {
	switch strings.ToLower(strings.TrimSpace(c.release)) {
	case "major":
		return major
	case "minor":
		return minor
	case "none":
		return none
	case "":
		for _, f := range c.files {
			if !strings.HasPrefix(f, "docs/") && path.Ext(f) != ".md" {
				return patch
			}
		}
		return none
	}
	return patch
}

// next is the tag after last for commits, false when they need no release
// or last is no vMAJOR.MINOR.PATCH tag. With no tag yet, last is v0.0.0.
func next(last string, commits []commit) (string, bool) {
	if last == "" {
		last = "v0.0.0"
	}
	parts := strings.Split(strings.TrimPrefix(last, "v"), ".")
	if len(parts) != 3 || !strings.HasPrefix(last, "v") {
		return "", false
	}
	var v [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return "", false
		}
		v[i] = n
	}
	bump := none
	for _, c := range commits {
		bump = max(bump, c.level())
	}
	switch bump {
	case none:
		return "", false
	case major:
		v = [3]int{v[0] + 1, 0, 0}
	case minor:
		v = [3]int{v[0], v[1] + 1, 0}
	default:
		v[2]++
	}
	return fmt.Sprintf("v%d.%d.%d", v[0], v[1], v[2]), true
}

type run func(args ...string) (string, error)

func gitIn(dir string) run {
	return func(args ...string) (string, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}
}

func plan(git run) (string, bool, error) {
	// With no tag yet describe fails and prints nothing: last is "".
	last, _ := git("describe", "--tags", "--abbrev=0", "--match", "v[0-9]*")
	rng := "HEAD"
	if last != "" {
		rng = last + "..HEAD"
	}
	ids, err := git("rev-list", rng)
	if err != nil {
		return "", false, err
	}
	var commits []commit
	for id := range strings.FieldsSeq(ids) {
		release, _ := git("log", "-1", "--format=%(trailers:key=Release,valueonly)", id)
		files, _ := git("diff-tree", "--no-commit-id", "--name-only", "-r", "--root", id)
		commits = append(commits, commit{release: release, files: strings.Fields(files)})
	}
	tag, ok := next(last, commits)
	return tag, ok, nil
}

func main() {
	tag, ok, err := plan(gitIn("."))
	if err != nil { //allele:ignore main only reports plan's error, which TestPlan covers
		fmt.Fprintln(os.Stderr, "nextversion:", err)
		os.Exit(1) //allele:ignore as above
	}
	if ok {
		fmt.Println(tag)
	}
}
