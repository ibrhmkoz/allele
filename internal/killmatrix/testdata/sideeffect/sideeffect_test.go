package sideeffect

import "testing"

func TestLoad(t *testing.T) {
	if got, _ := Load("abc"); got.n != 3 {
		t.Fatal(got.n)
	}
}
