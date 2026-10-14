package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestFacePreparationPinsRefuseInvalidMetadata(t *testing.T) {
	proof := manualPinTestProof()
	original := [2]defectEvidenceObject{proof.objects.Root, proof.objects.Params}
	buckets := [2]string{"original-bucket", "original-bucket"}
	if !validFacePreparationObjects(original, buckets, buckets[0], proof.binding.Source) {
		t.Fatal("original pins")
	}
	for _, mutate := range []func(*[2]defectEvidenceObject){
		func(p *[2]defectEvidenceObject) { p[1].ref.ArtifactID = p[0].ref.ArtifactID },
		func(p *[2]defectEvidenceObject) { p[0].ref.ArtifactID = proof.binding.Source.ArtifactID },
		func(p *[2]defectEvidenceObject) { p[1].ref.ArtifactID = uuid.Nil },
		func(p *[2]defectEvidenceObject) { p[1].ref.SHA256 = "invalid" },
		func(p *[2]defectEvidenceObject) { p[1].object.VersionID = "null" },
		func(p *[2]defectEvidenceObject) { p[0].object.Key = "" },
		func(p *[2]defectEvidenceObject) { p[1].object.Bytes = 1048577 },
		func(p *[2]defectEvidenceObject) { p[1].object.Bytes = 1 },
	} {
		changed := original
		mutate(&changed)
		if validFacePreparationObjects(changed, buckets, buckets[0], proof.binding.Source) {
			t.Fatal("invalid face pins accepted")
		}
	}
	if validFacePreparationObjects(original, [2]string{buckets[0], "foreign"}, buckets[0], proof.binding.Source) {
		t.Fatal("foreign bucket accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pins, err := (SQLTransferSourceReader{}).facePreparationObjects(ctx, auth.Identity{}, uuid.Nil, v2input.SessionVersionBinding{}, v2input.Ref{}, "face.detect")
	if !errors.Is(err, context.Canceled) || pins != ([2]defectEvidenceObject{}) {
		t.Fatal("cancel reached catalog", err)
	}
}
