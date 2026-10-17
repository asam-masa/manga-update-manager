package platform

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

type coverTestCloser struct {
	err   error
	calls int
}

func (c *coverTestCloser) Close() error { c.calls++; return c.err }

func TestCoverReleaseClassification(t *testing.T) {
	failure := errors.New("synthetic close failure")
	for _, tc := range []struct {
		name                   string
		closeErr, operationErr error
		want                   bool
	}{
		{"success", nil, nil, false},
		{"operation-only", nil, failure, false},
		{"cancel-only", nil, context.Canceled, false},
		{"close", failure, nil, true},
		{"cancel-and-close", failure, context.Canceled, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			closer := &coverTestCloser{err: tc.closeErr}
			err := errors.Join(tc.operationErr, closeCoverResource(closer))
			if closer.calls != 1 || errors.Is(err, ErrCoverRelease) != tc.want {
				t.Fatalf("classification: %v / calls=%d", err, closer.calls)
			}
			if tc.closeErr != nil && !errors.Is(err, tc.closeErr) {
				t.Fatal("lost close cause")
			}
			if tc.operationErr != nil && !errors.Is(err, tc.operationErr) {
				t.Fatal("lost operation cause")
			}
			if err != nil && errors.Is(fmt.Errorf("operation: %w", err), ErrCoverRelease) != tc.want {
				t.Fatal("wrapping lost marker")
			}
		})
	}
}
