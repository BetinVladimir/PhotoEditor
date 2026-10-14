package v2region

import (
	"context"
	"errors"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type regionClosingCursor struct {
	calls int
	err   error
}

func (c *regionClosingCursor) Next(context.Context) (v2input.Entry, bool, error) {
	return v2input.Entry{}, false, nil
}
func (c *regionClosingCursor) Close() error { c.calls++; return c.err }

func TestRegionInputCursorCloseFailurePreventsSuccessfulProof(t *testing.T) {
	closeErr := errors.New("cursor close failed")
	buildErr := errors.New("closure build failed")
	for _, prior := range []error{nil, buildErr} {
		cursor := &regionClosingCursor{err: closeErr}
		err := finishRegionInputCursor(cursor, prior)
		expected := closeErr
		if prior != nil {
			expected = prior
		}
		if !errors.Is(err, expected) || cursor.calls != 1 {
			t.Fatal("cursor error lost or close skipped", err, cursor.calls)
		}
	}
	cursor := &regionClosingCursor{}
	if err := finishRegionInputCursor(cursor, nil); err != nil || cursor.calls != 1 {
		t.Fatal("successful close rejected", err)
	}
}
