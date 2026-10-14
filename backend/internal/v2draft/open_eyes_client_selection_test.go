package v2draft

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestOpenEyesSelectionRejectsClientOwnedAuthority(t *testing.T) {
	ref := func() v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	}
	request := OpenEyesClientSelection{SchemaVersion: 1, Shoot: uuid.New(), Snapshot: uuid.New(), Ordinal: 0,
		Base:     v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		Donor:    v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},
		Strength: .5, EyesMask: ref(), Protected: ref(), TargetPixel: ref(), ProtectedPixel: ref(), SourceFaces: ref(), DonorFaces: ref(), SourceObservation: uuid.New(), DonorObservation: uuid.New()}
	raw, err := canonical.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DecodeOpenEyesClientSelection(raw); err != nil {
		t.Fatal(err)
	}
	request.ProtectedPixel = request.TargetPixel
	bad, err := canonical.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DecodeOpenEyesClientSelection(bad); err == nil {
		t.Fatal("duplicate physical mask ref accepted")
	}
	request.ProtectedPixel = ref()
	extra, err := canonical.Marshal(map[string]any{"schema_version": 1, "shoot_session_id": request.Shoot, "snapshot_id": request.Snapshot, "ordinal": 0, "base": request.Base, "donor": request.Donor, "strength": .5, "eyes_mask": request.EyesMask, "protected": request.Protected, "target_pixel": request.TargetPixel, "protected_pixel": request.ProtectedPixel, "source_faces": request.SourceFaces, "donor_faces": request.DonorFaces, "source_observation_id": request.SourceObservation, "donor_observation_id": request.DonorObservation, "confirmation": json.RawMessage(`{"artifact_id":"00000000-0000-4000-8000-000000000001","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DecodeOpenEyesClientSelection(extra); err == nil {
		t.Fatal("client-owned confirmation accepted")
	}
}
