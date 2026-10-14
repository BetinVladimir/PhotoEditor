package v2draft

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

func TestOpenEyesDraftBeginBindsExactProposalAndOwner(t *testing.T) {
	sha := strings.Repeat("a", 64)
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: sha} }
	tenant, actor := uuid.New(), uuid.New()
	key := "eyes-begin-1"
	run, _ := v2input.RunID(tenant, actor, key)
	workflow, _ := v2input.WorkflowID(run)
	owner, _ := v2input.TaskID(workflow, "portrait.open_eyes", 2)
	request := OpenEyesDraftBeginRequest{Tenant: tenant, Project: uuid.New(), Key: key,
		Actor: auth.Identity{UserID: actor, SessionID: uuid.New()},
		Pin: v2portrait.DonorQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(),
			PolicySHA: sha, Report: ref(), Calibration: ref(), ModelLockSHA: sha,
			RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha,
			IssuerID: uuid.New(), Backend: "cpu"}, ReservedBytes: minRestorationDraftReservation,
		Intent: OpenEyesClientIntent{SchemaVersion: 1, Shoot: uuid.New(), Snapshot: uuid.New(), Ordinal: 2,
			SourceObservation: uuid.New(), DonorObservation: uuid.New(), TargetPixel: ref(), ProtectedPixel: ref(),
			Proposal: v2portrait.OpenEyesParams{SchemaVersion: 1, Strength: .5,
				Base:         v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: sha},
				Donor:        v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: sha},
				SourceFaceID: uuid.New(), DonorFaceID: uuid.New(), EyesMask: ref(), Protected: ref(),
				SourceFaces: ref(), DonorFaces: ref(), Confirmation: v2input.Ref{ArtifactID: owner, SHA256: sha}}}}
	tx := &restorationStoreTx{role: "photo_scheduler", accepted: true}
	got, err := (SQLOpenEyesDraftBeginner{DB: restorationStoreDB{tx}}).Begin(context.Background(), request)
	if err != nil || got.Run != run || got.Workflow != workflow || got.Owner != owner || tx.commits != 1 || len(tx.args) != 4 {
		t.Fatal("open eyes draft identity or transaction mismatch", err)
	}
	var config struct {
		Intent struct {
			Operation string    `json:"operation"`
			Run       uuid.UUID `json:"run_id"`
			Workflow  uuid.UUID `json:"workflow_id"`
			IntentSHA string    `json:"client_intent_sha"`
		} `json:"v28_open_eyes"`
	}
	intentSHA, err := canonical.Hash(request.Intent)
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal([]byte(tx.args[0].(string)), &config) != nil ||
		config.Intent.Operation != "portrait.open_eyes" || config.Intent.Run != run ||
		config.Intent.Workflow != workflow || config.Intent.IntentSHA != intentSHA || tx.args[2] != sha ||
		tx.args[3] != minRestorationDraftReservation {
		t.Fatal("durable configuration lost operation or resource pin")
	}
	request.Intent.Proposal.Confirmation.ArtifactID = uuid.New()
	other := &restorationStoreTx{role: "photo_scheduler", accepted: true}
	if _, err := (SQLOpenEyesDraftBeginner{DB: restorationStoreDB{other}}).Begin(context.Background(), request); err == nil || len(other.args) != 0 {
		t.Fatal("changed owner reached database")
	}
}
