package slowbatch

import (
	"testing"
	"time"
)

func TestDouble1(t *testing.T) {
	time.Sleep(20 * time.Millisecond)
	if got := Double(2); got != 4 {
		t.Fatal(got)
	}
}

func TestDouble2(t *testing.T) {
	time.Sleep(20 * time.Millisecond)
	if got := Double(2); got != 4 {
		t.Fatal(got)
	}
}

func TestDouble3(t *testing.T) {
	time.Sleep(20 * time.Millisecond)
	if got := Double(2); got != 4 {
		t.Fatal(got)
	}
}

func TestDouble4(t *testing.T) {
	time.Sleep(20 * time.Millisecond)
	if got := Double(2); got != 4 {
		t.Fatal(got)
	}
}

func TestDouble5(t *testing.T) {
	time.Sleep(20 * time.Millisecond)
	if got := Double(2); got != 4 {
		t.Fatal(got)
	}
}

func TestDouble6(t *testing.T) {
	time.Sleep(20 * time.Millisecond)
	if got := Double(2); got != 4 {
		t.Fatal(got)
	}
}
