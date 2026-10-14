package v2draft

import (
	"testing"

	"github.com/google/uuid"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

func TestOpenEyesIntentOwnerAndPhysicalSelection(t *testing.T) {
	ref := func() v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	}
	tenant, actor := uuid.New(), uuid.New()
	run, err := v2input.RunID(tenant, actor, "open-eyes-attempt")
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := v2input.TaskID(workflow, "portrait.open_eyes", 3)
	if err != nil {
		t.Fatal(err)
	}
	base := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	donor := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}
	intent := OpenEyesClientIntent{SchemaVersion: 1, Shoot: uuid.New(), Snapshot: uuid.New(), Ordinal: 3,
		Proposal: v2portrait.OpenEyesParams{SchemaVersion: 1, Base: base, Donor: donor,
			Strength: .5, SourceFaceID: uuid.New(), DonorFaceID: uuid.New(),
			EyesMask: ref(), Protected: ref(), SourceFaces: ref(), DonorFaces: ref(),
			Confirmation: v2input.Ref{ArtifactID: owner, SHA256: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}},
		TargetPixel: ref(), ProtectedPixel: ref(), SourceObservation: uuid.New(), DonorObservation: uuid.New()}
	raw, err := canonical.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeOpenEyesClientIntent(raw)
	if err != nil {
		t.Fatal(err)
	}
	if gotRun, gotWorkflow, err := VerifyOpenEyesIntentOwner(decoded, tenant, actor, "open-eyes-attempt"); err != nil || gotRun != run || gotWorkflow != workflow {
		t.Fatal("correct owner rejected")
	}
	if _, _, err := VerifyOpenEyesIntentOwner(decoded, tenant, actor, "other-attempt"); err == nil {
		t.Fatal("different idempotency key accepted")
	}
	intent.ProtectedPixel = intent.TargetPixel
	bad, _ := canonical.Marshal(intent)
	if _, err := DecodeOpenEyesClientIntent(bad); err == nil {
		t.Fatal("duplicate physical reference accepted")
	}
	if _, err := DecodeOpenEyesClientIntent(append(append([]byte(nil), raw...), ' ')); err == nil {
		t.Fatal("noncanonical JSON accepted")
	}
}
