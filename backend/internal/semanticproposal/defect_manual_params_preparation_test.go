package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestFreezeCompiledDefectParamsOwnsCompilerBuffers(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if err = json.Unmarshal(raw, &images); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(images[0])
	compiled, err := review.BuildDefectProposalTaskInputsFromImage(context.Background(), review.ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(digest[:])}, images[0], []review.DefectProposalFaceInput{}, [2]float64{.002, .012}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := freezeCompiledDefectParams(context.Background(), compiled)
	if err != nil {
		t.Fatal(err)
	}
	original := prepared
	compiled.Params[0] = ' '
	compiled.Inputs[0].SHA256 = "changed"
	if prepared != original || prepared.params[0] != '{' {
		t.Fatal("compiler buffer alias escaped")
	}
	if _, err = freezeCompiledDefectParams(context.Background(), compiled); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("changed compiler digest accepted", err)
	}
	if _, err = freezeCompiledDefectParams(context.Background(), review.DefectProposalTaskInputs{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("empty compiler output accepted", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := freezeCompiledDefectParams(ctx, compiled)
	if !errors.Is(err, context.Canceled) || got != (PreparedDefectParams{}) {
		t.Fatal("cancelled preparation returned payload", err)
	}
}
