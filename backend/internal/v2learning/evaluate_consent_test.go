package v2learning

import (
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestFrozenEvaluateConsentAllowsSharedSnapshotAndRequiresMatchingGrant(t *testing.T) {
	version, person, consent, face := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	binding := v2input.SessionVersionBinding{AssetVersionID: version}
	emptyFaces := []byte(`{"faces":[]}`)
	build := func(asset uuid.UUID, epoch string) []byte {
		body, err := canonical.Marshal(map[string]any{
			"schema_version": 1,
			"grants": []any{map[string]any{"consent_id": consent,
				"epoch": "3", "person_id": person,
				"purpose": "identity_matching"}},
			"confirmed_faces": []any{map[string]any{
				"asset_version_id": asset, "face_id": face,
				"person_id": person, "consent_id": consent,
				"epoch": epoch}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	if err := ValidateFrozenEvaluateConsent(build(version, "3"), emptyFaces, binding); err != nil {
		t.Fatal(err)
	}
	if err := ValidateFrozenEvaluateConsent(build(uuid.New(), "3"), emptyFaces, binding); err != nil {
		t.Fatalf("shared snapshot rejected: %v", err)
	}
	if err := ValidateFrozenEvaluateConsent(build(version, "4"), emptyFaces, binding); err == nil {
		t.Fatal("mismatched consent epoch accepted")
	}
	faces, err := canonical.Marshal(map[string]any{"faces": []any{
		map[string]any{"face_id": face, "person_id": person,
			"embedding": make([]float64, 512)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateFrozenEvaluateConsent(build(version, "3"), faces, binding); err != nil {
		t.Fatal(err)
	}
	if err := ValidateFrozenEvaluateConsent(build(uuid.New(), "3"), faces, binding); err == nil {
		t.Fatal("unconfirmed source face accepted")
	}
}
