package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestAutomaticOriginalManualProtectionInputs(t *testing.T) {
	raw, err := os.ReadFile("testdata/face-manual-root-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var document rasterizedFaceMask
	if err := decodeExact(raw, &document); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	ref := ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	source := DefectSource{Pixels: ArtifactRef{ID: uuid.New(), SHA256: document.Image.SHA256}, Image: document.Image, Canvas: document.Target.Canvas, TransformChain: document.Target.TransformChain}
	original := OriginalManualProtection{Root: ref, Body: raw, Target: document.Target}
	task, err := BuildDefectProposalTaskInputs(context.Background(), source, []DefectProposalFaceInput{}, [2]float64{.002, .012}, nil, nil, []OriginalManualProtection{original})
	if err != nil || len(task.Inputs) != 4 || len(task.ManualRoots) != 1 {
		t.Fatal("manual task dependencies missing", task, err)
	}
	var taskParams proposalReportParams
	if decodeExact(task.Params, &taskParams) != nil || taskParams.Protections == nil || len(*taskParams.Protections) != 1 || (*taskParams.Protections)[0].Artifact != document.Protected.Artifact {
		t.Fatal("original protected child missing from Params")
	}
	permanent := document.Protected
	result, err := BuildDefectProtectionInputs(context.Background(), source, &permanent, nil, []OriginalManualProtection{original, original})
	if err != nil || result.Permanent == nil || len(result.Protections) != 0 || len(result.Inputs) != 1 || len(result.Roots) != 1 || result.Inputs[0] != document.Protected.Artifact {
		t.Fatal(result, err)
	}
	*result.Permanent.ObjectID = uuid.New()
	if *result.Permanent.ObjectID == *document.Protected.ObjectID {
		t.Fatal("protection ownership shared")
	}
	for _, id := range []uuid.UUID{source.Image.ID, ref.ID} {
		aliased := document.Protected
		aliased.Artifact.ID = id
		denied, err := BuildDefectProtectionInputs(context.Background(), source, nil, []ProtectionMask{aliased}, []OriginalManualProtection{original})
		if err == nil || denied.Inputs != nil || denied.Roots != nil || denied.Protections != nil || denied.Permanent != nil {
			t.Fatal("cross-role artifact alias accepted", id, denied, err)
		}
	}
	rootAlias := original
	rootAlias.Root.ID = source.Image.ID
	if denied, err := BuildDefectProtectionInputs(context.Background(), source, nil, nil, []OriginalManualProtection{rootAlias}); err == nil || denied.Roots != nil {
		t.Fatal("source/root alias accepted", denied, err)
	}
	extra := document.Protected
	extra.Artifact.ID = uuid.New()
	a, err := BuildDefectProtectionInputs(context.Background(), source, nil, []ProtectionMask{extra, document.Protected}, []OriginalManualProtection{original})
	if err != nil {
		t.Fatal(err)
	}
	b, err := BuildDefectProtectionInputs(context.Background(), source, nil, []ProtectionMask{document.Protected, extra}, []OriginalManualProtection{original})
	if err != nil {
		t.Fatal(err)
	}
	left, _ := canonical.Marshal(a)
	right, _ := canonical.Marshal(b)
	if string(left) != string(right) || len(a.Protections) != 2 {
		t.Fatal("unstable protection ordering")
	}
	changed := document.Protected
	changed.Confidence = .5
	if denied, err := BuildDefectProtectionInputs(context.Background(), source, nil, []ProtectionMask{changed}, []OriginalManualProtection{original}); err == nil || denied.Inputs != nil {
		t.Fatal("conflicting metadata accepted")
	}
	bad := original
	bad.Body = append(append([]byte{}, raw...), ' ')
	if denied, err := BuildDefectProtectionInputs(context.Background(), source, nil, nil, []OriginalManualProtection{bad}); err == nil || denied.Roots != nil {
		t.Fatal("changed original root accepted")
	}
	tooMany := make([]ProtectionMask, 64)
	for i := range tooMany {
		tooMany[i] = document.Protected
		tooMany[i].Artifact.ID = uuid.New()
	}
	if denied, err := BuildDefectProtectionInputs(context.Background(), source, nil, tooMany, []OriginalManualProtection{original}); err == nil || denied.Protections != nil {
		t.Fatal("protection set silently truncated")
	}
	oversized := original
	oversized.Body = make([]byte, 4194305)
	if denied, err := BuildDefectProtectionInputs(context.Background(), source, nil, nil, []OriginalManualProtection{oversized}); err == nil || denied.Roots != nil {
		t.Fatal("metadata budget bypass")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if denied, err := BuildDefectProtectionInputs(ctx, source, nil, nil, nil); !errors.Is(err, context.Canceled) || denied.Inputs != nil {
		t.Fatal(denied, err)
	}
}
