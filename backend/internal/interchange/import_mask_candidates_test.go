package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type importMaskCandidateArchiveStub struct {
	body  []byte
	calls int
}

func (s *importMaskCandidateArchiveStub) Read(_ context.Context, _ v2input.Ref, contract string) ([]byte, error) {
	s.calls++
	if contract != "EditingRecipeV3" {
		return nil, v2input.ErrInvalid
	}
	return append([]byte(nil), s.body...), nil
}

func TestImportMaskCandidatesRequireExactEntryAndRecipe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ExtractImportPersonMaskCandidates(ctx, nil, uuid.New(), uuid.New(), &importMaskCandidateArchiveStub{}); !errors.Is(err, context.Canceled) {
		t.Fatal("lost cancellation", err)
	}
	parsed := importParsedValidatorFixture()
	archive := &importMaskCandidateArchiveStub{}
	raw, err := canonical.Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExtractImportPersonMaskCandidates(context.Background(), raw, parsed.Manifest.Entries[0].ID, uuid.New(), archive); err == nil || archive.calls != 0 {
		t.Fatal("entry without recipe opened archive", err)
	}
	if _, err := ExtractImportPersonMaskCandidates(context.Background(), raw, uuid.New(), uuid.New(), archive); err == nil || archive.calls != 0 {
		t.Fatal("foreign entry opened archive", err)
	}
}
