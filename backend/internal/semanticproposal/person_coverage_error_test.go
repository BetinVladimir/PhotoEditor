package semanticproposal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/v2qualification"
	"testing"
)

func TestPersonCoverageOnlyMissingSupportIsUnqualified(t *testing.T) {
	missing := fmt.Errorf("coverage: %w", masknpy.ErrCoverageMissing)
	err := classifyPersonCoverageError(missing)
	if !errors.Is(err, masknpy.ErrCoverageMissing) || !errors.Is(err, v2qualification.ErrUnqualified) {
		t.Fatal(err)
	}
	for _, original := range []error{nil, context.Canceled, context.DeadlineExceeded, io.ErrUnexpectedEOF, masknpy.ErrInvalid, errors.New("storage failure")} {
		got := classifyPersonCoverageError(original)
		if got != original || errors.Is(got, v2qualification.ErrUnqualified) {
			t.Fatal("corruption/IO/cancellation reclassified", original, got)
		}
	}
}
