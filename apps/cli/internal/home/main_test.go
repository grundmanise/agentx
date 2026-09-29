package home

import (
	"os"
	"testing"
)

// TestMain turns off the flushes to the disk, as the CLI's tests do: the
// journal tests write and recover dozens of journals, and no test here is
// about whether a write reached the disk.
func TestMain(m *testing.M) {
	SkipFlushesInTests()
	os.Exit(m.Run())
}
