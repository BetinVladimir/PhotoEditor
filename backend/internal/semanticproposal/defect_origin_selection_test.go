package semanticproposal

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestPreparedDefectSelectionRejectsAnotherExecutionWithSameParams(t *testing.T) {
	selected := defectOriginSelection{ProducerID: uuid.New(), ParamsSHA: strings.Repeat("a", 64)}
	params := v2input.Ref{ArtifactID: uuid.New(), SHA256: selected.ParamsSHA}
	config := func(value any) []byte {
		body, err := canonical.Marshal(map[string]any{"v27_defect_prepare": map[string]any{"original_producer": value}})
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	if err := validatePreparedDefectSelection(config(selected), selected, params); err != nil {
		t.Fatal("exact owned selection", err)
	}
	other := selected
	other.ProducerID = uuid.New()
	if err := validatePreparedDefectSelection(config(other), selected, params); !errors.Is(err, v2input.ErrConflict) {
		t.Fatal("different same-Params execution accepted", err)
	}
	other = selected
	other.ParamsSHA = strings.Repeat("b", 64)
	if err := validatePreparedDefectSelection(config(other), selected, params); !errors.Is(err, v2input.ErrConflict) {
		t.Fatal("changed saved Params accepted", err)
	}
	params.SHA256 = other.ParamsSHA
	if err := validatePreparedDefectSelection(config(selected), selected, params); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("original Params substitution accepted", err)
	}
}

func TestDefectOriginSelectionRejectsMissingPinsBeforeSQL(t *testing.T) {
	// Touching the uninitialized pool would panic. A restart without a complete
	// original result must not guess a producer from partially retained metadata.
	r := SQLTransferSourceReader{DB: &pgxpool.Pool{}, ProjectID: uuid.New()}
	b := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New()}}
	got, err := r.selectDefectOriginProducer(context.Background(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), b, DefectOriginPins{})
	if !errors.Is(err, v2input.ErrInvalid) || got != (defectOriginSelection{}) {
		t.Fatal("incomplete provenance entered SQL or returned a selection", err)
	}
}

func TestDefectOriginSelectionCancelledBeforeSQL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := SQLTransferSourceReader{DB: &pgxpool.Pool{}}
	got, err := r.selectDefectOriginProducer(ctx, auth.Identity{}, uuid.Nil, v2input.SessionVersionBinding{}, DefectOriginPins{})
	if !errors.Is(err, context.Canceled) || got != (defectOriginSelection{}) {
		t.Fatal("cancelled selection entered SQL or returned a selection", err)
	}
}
