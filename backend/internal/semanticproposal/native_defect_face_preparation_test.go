package semanticproposal

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
)

func TestNativeDefectFacesBindOriginalObjectsAndOwnMask(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/face68-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var face review.Face68
	if err := json.Unmarshal(raw, &face); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile("../review/testdata/face-skin-root-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var mask review.ProtectionMask
	if err := json.Unmarshal(raw, &mask); err != nil {
		t.Fatal(err)
	}
	mask.ObjectID = nil
	model := strings.Repeat("a", 64)
	mask.ModelLockID = &model
	landmarks := review.ArtifactRef{ID: uuid.New(), SHA256: mask.Artifact.SHA256}
	second := face
	second.ObjectID = uuid.New()
	faces := []review.Face68{face, second}
	got, err := bindNativeDefectFaceInputs(context.Background(), faces, landmarks, mask, review.ArtifactRef{})
	if err != nil || len(got) != 2 {
		t.Fatal("nonempty native faces", err)
	}
	for i, input := range got {
		if input.ObjectID == nil || *input.ObjectID != faces[i].ObjectID || input.Skin.ObjectID == nil || *input.Skin.ObjectID != faces[i].ObjectID || input.Landmarks != landmarks || input.Skin.Artifact != mask.Artifact || input.Skin.Canvas != mask.Canvas || input.Skin.Confidence != mask.Confidence {
			t.Fatal("changed original face/mask binding")
		}
	}
	originalModel := *mask.ModelLockID
	originalGeometry := append([]byte(nil), mask.TransformChain...)
	*got[0].Skin.ModelLockID = "mutated"
	got[0].Skin.TransformChain[0] = 'x'
	if *mask.ModelLockID != originalModel || *got[1].Skin.ModelLockID != originalModel || string(mask.TransformChain) != string(originalGeometry) || string(got[1].Skin.TransformChain) != string(originalGeometry) {
		t.Fatal("bound mask aliases retained evidence")
	}
	duplicate := []review.Face68{face, face}
	if denied, err := bindNativeDefectFaceInputs(context.Background(), duplicate, landmarks, mask, review.ArtifactRef{}); err == nil || denied != nil {
		t.Fatal("duplicate original object accepted")
	}
	changed := face
	changed.Canvas.Width++
	if denied, err := bindNativeDefectFaceInputs(context.Background(), []review.Face68{changed}, landmarks, mask, review.ArtifactRef{}); err == nil || denied != nil {
		t.Fatal("foreign canvas accepted")
	}
	alias := landmarks
	alias.ID = mask.Artifact.ID
	if denied, err := bindNativeDefectFaceInputs(context.Background(), faces, alias, mask, review.ArtifactRef{}); err == nil || denied != nil {
		t.Fatal("landmark/mask alias accepted")
	}
}

func TestNativeDefectPreparedParamsRefusesEmptyAndCancelledProof(t *testing.T) {
	if err := verifyNativeDefectPreparedParams(context.Background(), nativeDefectFacePreparation{}); err == nil {
		t.Fatal("empty original Params accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verifyNativeDefectPreparedParams(ctx, nativeDefectFacePreparation{}); err != context.Canceled {
		t.Fatal("cancelled Params binding accessed producer", err)
	}
}
