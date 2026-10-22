package v2sessions

import (
	"context"
	"errors"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

func TestAnalysisCorrectionVectorsRespectDispositionAndSource(t *testing.T) {
	const snapshotSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const sourceA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const sourceB = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	bindA := v2input.SessionVersionBinding{AssetVersionID: uuid.New(),
		Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: sourceA}, CanvasHash: snapshotSHA}
	bindB := v2input.SessionVersionBinding{AssetVersionID: uuid.New(),
		Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: sourceB}, CanvasHash: snapshotSHA}
	bindings := map[int]v2input.SessionVersionBinding{0: bindA, 1: bindB}
	applied := v2draft.CorrectionVector{EV: .25, SaturationDelta: .1}
	locked := v2draft.CorrectionVector{}
	doc := map[string]any{"schema_version": 1, "snapshot_digest": snapshotSHA,
		"items": []any{
			map[string]any{"binding": bindA, "ordinal": 0, "delta": applied,
				"disposition": "apply", "reason": nil},
			map[string]any{"binding": bindB, "ordinal": 1, "delta": locked,
				"disposition": "manual_locked", "reason": "manual pick"},
		}}
	raw, err := canonical.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	sha, err := canonical.Hash(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	group := sceneGroup{ID: uuid.New(), Ordinals: []int{0, 1},
		ReferenceOrdinal: 0, Corrections: ArtifactRef{ArtifactID: uuid.New(), SHA256: sha}}
	vectors, err := correctionVectors(raw, group, snapshotSHA, bindings)
	if err != nil || vectors[0] != applied || vectors[1] != locked {
		t.Fatalf("invalid corrections: %+v, %v", vectors, err)
	}
	bindings[1] = bindA
	if _, err := correctionVectors(raw, group, snapshotSHA, bindings); err == nil {
		t.Fatal("correction accepted with swapped frozen source")
	}
}

// Cancellation must win before dependencies or admission are touched.
func TestApplyAnalysisCancelledBeforeAdmission(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ticket, replayed, err := (Service{}).ApplyAnalysis(ctx, auth.Identity{},
		uuid.New(), uuid.New(), "invalid", ApplyDecision{})
	if !errors.Is(err, context.Canceled) || replayed || ticket.RunID != uuid.Nil {
		t.Fatalf("cancelled Apply exposed a receipt: %+v, %v, %v", ticket, replayed, err)
	}
}
