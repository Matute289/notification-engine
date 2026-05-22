package sendgrid

import (
	"testing"
	"time"
)

// mustNow keeps the test files small.
func mustNow(t *testing.T) time.Time { t.Helper(); return time.Unix(1700000000, 0) }
