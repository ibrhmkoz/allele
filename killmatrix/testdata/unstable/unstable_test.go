package unstable

import (
	"strconv"
	"testing"
	"time"
)

// TestClockName names its subtest after the clock, so no two runs share a
// leaf and no column can line up across runs.
func TestClockName(t *testing.T) {
	t.Run(strconv.FormatInt(time.Now().UnixNano(), 36), func(t *testing.T) {
		if Double(2) != 4 {
			t.Fatal("wrong")
		}
	})
}

func TestDouble(t *testing.T) {
	if Double(3) != 6 {
		t.Fatal("wrong")
	}
}
