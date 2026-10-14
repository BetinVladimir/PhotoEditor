package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestNativeFaceStageIdentities(t *testing.T) {
	parent := uuid.New()
	runs := map[uuid.UUID]bool{}
	for _, operation := range []string{"face.landmarks", "face.parse"} {
		run, workflow, err := nativeFacePreparationIDs(parent, operation)
		if err != nil || run == uuid.Nil || workflow == uuid.Nil || runs[run] {
			t.Fatal("stage identity", err)
		}
		again, againWorkflow, err := nativeFacePreparationIDs(parent, operation)
		if err != nil || again != run || againWorkflow != workflow {
			t.Fatal("unstable stage identity")
		}
		runs[run] = true
		foreign, _, err := nativeFacePreparationIDs(uuid.New(), operation)
		if err != nil || foreign == run {
			t.Fatal("foreign parent alias")
		}
	}
	for _, operation := range []string{"", "face.detect", "mask.person", "face.parse:1"} {
		if _, _, err := nativeFacePreparationIDs(parent, operation); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal("unsupported stage identity")
		}
	}
	if _, _, err := nativeFacePreparationIDs(uuid.Nil, "face.parse"); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("nil parent")
	}
}
func TestNativeFaceTaskCancelledBeforeDependencyIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := (SQLTransferSourceReader{}).prepareNativeFaceTask(ctx, nil, auth.Identity{}, detectorDraftPlan{}, TransferColorProfile{}, nativeFaceStageParams{}, v2input.Ref{}, nativePreparationBudget{})
	if !errors.Is(err, context.Canceled) || result.frozen != nil {
		t.Fatal("cancelled preparation exposed task", err)
	}
}

func TestNativeFaceSourceEvidenceRejectsSubstitutedCatalog(t *testing.T) {
	source := v2input.Ref{ArtifactID: uuid.New(), SHA256: "source"}
	faces := v2input.Ref{ArtifactID: uuid.New(), SHA256: "faces"}
	params := v2input.Ref{ArtifactID: uuid.New(), SHA256: "params"}
	object := objectstore.Object{Key: "source", VersionID: "v1", SHA256: source.SHA256, Bytes: 128}
	pins, err := canonical.Marshal([]sourcePreparationPin{{Node: v2draft.LayerRef{Ref: source, Contract: "WorkingImage"}, Object: object}})
	if err != nil {
		t.Fatal(err)
	}
	proof := sourcePreparationProof{pins: string(pins)}
	entries := []v2draft.InputEvidence{{Ref: source, Contract: "WorkingImage", Object: object}, {Ref: faces, Contract: "FaceBoxes"}, {Ref: params, Contract: "Params"}}
	if err := verifyNativeFaceSourceEvidence(context.Background(), proof, faces, params, entries); err != nil {
		t.Fatal("exact source evidence", err)
	}
	for _, kind := range []string{"bytes", "version", "contract", "missing", "duplicate", "foreign"} {
		changed := append([]v2draft.InputEvidence(nil), entries...)
		switch kind {
		case "bytes":
			changed[0].Object.Bytes--
		case "version":
			changed[0].Object.VersionID = "v2"
		case "contract":
			changed[0].Contract = "Params"
		case "missing":
			changed = changed[1:]
		case "duplicate":
			changed[1] = changed[0]
		case "foreign":
			changed[0].Ref.ArtifactID = uuid.New()
		}
		if err := verifyNativeFaceSourceEvidence(context.Background(), proof, faces, params, changed); err == nil {
			t.Fatal("substituted source accepted", kind)
		}
	}
}
