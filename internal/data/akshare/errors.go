package akshare

import (
	"errors"
	"fmt"
)

// Sentinel errors for clear degradation (no panics).
var (
	// ErrSkipped is returned when network/CLI access is disabled and no cache hit.
	ErrSkipped = errors.New("akshare: network/CLI skipped (use fixture provider or populate CacheDir)")

	// ErrHelperFailed wraps non-zero helper exits / IO failures.
	ErrHelperFailed = errors.New("akshare: helper failed")

	// ErrAKShareImport indicates the Python helper could not import akshare.
	ErrAKShareImport = errors.New("akshare: python package not installed (pip install akshare)")

	// ErrNotCached is returned when CacheDir is required but the object is missing.
	ErrNotCached = errors.New("akshare: not found in cache")
)

func wrapHelper(err error, detail string) error {
	if err == nil {
		return fmt.Errorf("%w: %s", ErrHelperFailed, detail)
	}
	return fmt.Errorf("%w: %s: %v", ErrHelperFailed, detail, err)
}
