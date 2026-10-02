package v2input

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func TestCaptureContextEvidenceBindsDocumentsAndExcludesCurrentFrame(t *testing.T) {
	current, person, grant, requirement := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	shots := CaptureShotList{1, []CaptureShotRequirement{{requirement, "Portrait", []uuid.UUID{person}, 1, .7}}}
	sha, _ := canonical.Hash(shots)
	shotsRef := Ref{uuid.New(), sha}
	seed := CaptureSelectionSeed{1, uuid.New(), "1", "1", &CaptureShotDocument{uuid.New(), shotsRef.ArtifactID, sha}, []string{strings.Repeat("a", 64)}}
	consent := map[string]any{"schema_version": 1, "grants": []sessionConsentGrant{{grant, "1", &person, "identity_matching"}}, "confirmed_faces": []sessionConfirmedFace{}}
	body, _ := canonical.Marshal(consent)
	sha, _ = canonical.Hash(json.RawMessage(body))
	consentsRef := Ref{uuid.New(), sha}
	quality := .8
	frames := []CaptureConfirmedFrame{{uuid.New(), []uuid.UUID{person}, &quality}}
	raw, err := CompileCaptureContextEvidence(seed, current, shots, shotsRef, body, consentsRef, frames)
	if err != nil {
		t.Fatal(err)
	}
	var context CaptureContext
	if json.Unmarshal(raw, &context) != nil || len(context.ConfirmedPersonIDs) != 0 || len(context.Coverage) != 1 || context.Coverage[0].ConfirmedFrames != 1 {
		t.Fatalf("context=%s", raw)
	}
	frames[0].AssetVersionID = current
	if _, err = CompileCaptureContextEvidence(seed, current, shots, shotsRef, body, consentsRef, frames); err == nil {
		t.Fatal("current frame counted as history")
	}
	frames[0].AssetVersionID = uuid.New()
	frames[0].Quality = nil
	raw, err = CompileCaptureContextEvidence(seed, current, shots, shotsRef, body, consentsRef, frames)
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(raw, &context)
	if context.Coverage[0].ConfirmedFrames != 0 {
		t.Fatal("unqualified quality counted")
	}
	consentsRef.SHA256 = strings.Repeat("c", 64)
	if _, err = CompileCaptureContextEvidence(seed, current, shots, shotsRef, body, consentsRef, frames); err == nil {
		t.Fatal("swapped consent document accepted")
	}
}
