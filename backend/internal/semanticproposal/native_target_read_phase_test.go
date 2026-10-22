package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestNativeTargetReadClonesOwnNestedProofs(t *testing.T) {
	id := uuid.New()
	model := "original"
	face := nativeTargetFaceResult{catalog: nativeFaceResultCatalog{Outputs: []v2input.Ref{{SHA256: "original"}}}, landmarks: []review.Face68{{Points: []review.Landmark{{X: .5}}, TransformChain: []byte("[]")}}, labels: review.FaceLabels19{TransformChain: []byte("[]"), Warnings: []string{"original"}}, detector: nativeTargetDetectorResult{completed: completedNativeDetector{boxes: review.FaceBoxes{Warnings: []string{"original"}, Faces: []review.FaceBox{{Confidence: .5}}}}}}
	mask := review.ProtectionMask{ObjectID: &id, ModelLockID: &model, TransformChain: []byte("[]")}
	skin := completedNativeTargetSkin{catalog: nativeSkinResultCatalog{Outputs: face.catalog.Outputs}, parser: face, mask: mask}
	defect := completedNativeTargetDefect{catalog: nativeInputDefectResultCatalog{Outputs: face.catalog.Outputs}, evidence: VerifiedDefectEvidence{Params: []byte("{}"), Proposal: []byte("{}"), Report: []byte("{}"), FaceMasks: []review.ProposalFaceMask{{ObjectID: &id, Mask: mask}}}}
	f := cloneNativeTargetFace(face)
	f.catalog.Outputs[0].SHA256 = "changed"
	f.landmarks[0].Points[0].X = 1
	f.landmarks[0].TransformChain[0] = 'x'
	f.labels.TransformChain[0] = 'x'
	f.labels.Warnings[0] = "changed"
	f.detector.completed.boxes.Faces[0].Confidence = 1
	f.detector.completed.boxes.Warnings[0] = "changed"
	s := cloneNativeTargetSkin(skin)
	s.catalog.Outputs[0].SHA256 = "changed"
	s.parser.labels.Warnings[0] = "changed"
	*s.mask.ObjectID = uuid.Nil
	*s.mask.ModelLockID = "changed"
	s.mask.TransformChain[0] = 'x'
	d := cloneNativeTargetDefect(defect)
	d.catalog.Outputs[0].SHA256 = "changed"
	d.evidence.Params[0] = 'x'
	d.evidence.Proposal[0] = 'x'
	d.evidence.Report[0] = 'x'
	*d.evidence.FaceMasks[0].ObjectID = uuid.Nil
	d.evidence.FaceMasks[0].Mask.TransformChain[0] = 'x'
	if face.catalog.Outputs[0].SHA256 != "original" || face.landmarks[0].Points[0].X != .5 || string(face.landmarks[0].TransformChain) != "[]" || face.labels.Warnings[0] != "original" || string(face.labels.TransformChain) != "[]" || face.detector.completed.boxes.Faces[0].Confidence != .5 || face.detector.completed.boxes.Warnings[0] != "original" || *skin.mask.ObjectID != id || *skin.mask.ModelLockID != "original" || string(skin.mask.TransformChain) != "[]" || string(defect.evidence.Params) != "{}" || string(defect.evidence.Proposal) != "{}" || string(defect.evidence.Report) != "{}" || *defect.evidence.FaceMasks[0].ObjectID != id || string(defect.evidence.FaceMasks[0].Mask.TransformChain) != "[]" {
		t.Fatal("target proof aliases consumer")
	}
}

func TestNativeTargetReadStageAndOrdinalSealsDoNotCollide(t *testing.T) {
	ctx := context.Background()
	who := phaseTestIdentity()
	s := SQLTransferSourceReader{}
	count := 0
	_, err := nativeRead(ctx, s, who, TransferColorProfile{}, nativeReadKey{"target-defect", "root"}, clonePhaseProof, func(r SQLTransferSourceReader) (phaseProof, error) {
		for _, key := range []nativeReadKey{{"detector", "same"}, {"target-detector", "same"}, {"target-face-face.parse", "ordinal0"}, {"target-face-face.parse", "ordinal1"}, {"target-face-face.landmarks", "ordinal0"}, {"target-face-face.parse", "ordinal0"}} {
			_, err := nativeRead(ctx, r, who, TransferColorProfile{}, key, clonePhaseProof, func(SQLTransferSourceReader) (phaseProof, error) { count++; return phaseProof{[]int{count}}, nil }, phaseTestWitness)
			if err != nil {
				return phaseProof{}, err
			}
		}
		return phaseProof{[]int{count}}, nil
	}, phaseTestWitness)
	if err != nil || count != 5 {
		t.Fatalf("builds=%d err=%v", count, err)
	}
}

func TestNativeTargetFaceManifestUsesCanonicalOperationOrdinal(t *testing.T) {
	first := v2input.Ref{ArtifactID: uuid.New(), SHA256: "first"}
	second := v2input.Ref{ArtifactID: uuid.New(), SHA256: "second"}
	for _, tc := range []struct {
		op    string
		refs  []v2input.Ref
		want  v2input.Ref
		valid bool
	}{
		{"face.landmarks", []v2input.Ref{first}, first, true},
		{"face.parse", []v2input.Ref{first, second}, second, true},
		{"face.parse", []v2input.Ref{second, first}, first, true},
		{"face.landmarks", nil, v2input.Ref{}, false},
		{"face.landmarks", []v2input.Ref{first, second}, v2input.Ref{}, false},
		{"face.parse", []v2input.Ref{first}, v2input.Ref{}, false},
		{"unknown", []v2input.Ref{first}, v2input.Ref{}, false},
	} {
		got, err := nativeTargetFaceManifest(tc.op, tc.refs)
		if (err == nil) != tc.valid || got != tc.want {
			t.Fatalf("%s refs=%d got=%v err=%v", tc.op, len(tc.refs), got, err)
		}
	}
}
