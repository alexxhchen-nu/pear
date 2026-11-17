package gateway

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the suite if any goroutine outlives a test, which is how the
// TTFT first-byte reader would leak if a body were never closed.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
