package v2draft

import (
	"testing"

	"github.com/google/uuid"

	"photoeditor/backend/internal/v2input"
)

func TestOpenEyesProvisionalBindsConfirmationToOwner(t *testing.T) {
	ref := func() v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	}
	selection := OpenEyesClientSelection{SchemaVersion: 1, Shoot: uuid.New(), Snapshot: uuid.New(),
		Base: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(),
			CanvasHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		Donor: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(),
			CanvasHash: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},
		Strength: .5, EyesMask: ref(), Protected: ref(), TargetPixel: ref(),
		ProtectedPixel: ref(), SourceFaces: ref(), DonorFaces: ref(),
		SourceObservation: uuid.New(), DonorObservation: uuid.New()}
	owner := uuid.New()
	params, err := openEyesProvisional(selection, owner)
	if err != nil {
		t.Fatal(err)
	}
	if params.Confirmation.ArtifactID != owner || params.EyesMask != selection.EyesMask ||
		params.Base != selection.Base || params.Donor != selection.Donor {
		t.Fatal("proposal did not preserve selected inputs or owner identity")
	}
	if _, err := openEyesProvisional(selection, uuid.Nil); err == nil {
		t.Fatal("nil owner accepted")
	}
}
