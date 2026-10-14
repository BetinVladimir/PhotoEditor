package semanticproposal

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestManualEvidencePinClosure(t *testing.T) {
	proof := manualPinTestProof()
	for _, external := range []bool{false, true} {
		expected := 6
		if external {
			vectors := proof.objects.Root
			vectors.ref.ArtifactID = uuid.New()
			vectors.object.Key = uuid.NewString()
			proof.vectors = &vectors
			expected = 7
		}
		body, err := marshalManualEvidencePins(proof, "original-bucket")
		if err != nil {
			t.Fatal(err)
		}
		var pins []manualEvidencePin
		if err = json.Unmarshal(body, &pins); err != nil || len(pins) != expected {
			t.Fatal("incomplete original evidence", err)
		}
		for i, p := range pins {
			if p.Bucket != "original-bucket" || p.Version != "original-version" || i > 0 && pins[i-1].Role >= p.Role {
				t.Fatal("unstable metadata/role order")
			}
		}
		again, err := marshalManualEvidencePins(proof, "original-bucket")
		if err != nil || string(again) != string(body) {
			t.Fatal("unstable evidence")
		}
	}
	for _, mutate := range []func(*manualPreparationProof){
		func(p *manualPreparationProof) { p.children[0].ref.ArtifactID = p.objects.Root.ref.ArtifactID },
		func(p *manualPreparationProof) { p.children[1].object.ContentType = "application/octet-stream" },
		func(p *manualPreparationProof) { p.objects.Params.object.VersionID = "null" },
		func(p *manualPreparationProof) { p.children[2].object.SHA256 = strings.Repeat("d", 64) },
		func(p *manualPreparationProof) { p.objects.Pixel.object.Bytes = 31 },
		func(p *manualPreparationProof) { p.objects.Root.ref.ArtifactID = p.binding.Source.ArtifactID },
	} {
		changed := proof
		mutate(&changed)
		if body, err := marshalManualEvidencePins(changed, "original-bucket"); err == nil || body != nil {
			t.Fatal("invalid evidence serialized")
		}
	}
	if body, err := marshalManualEvidencePins(manualPreparationProof{}, "original-bucket"); err == nil || body != nil {
		t.Fatal("empty proof serialized")
	}
}

func manualPinTestProof() manualPreparationProof {
	proof := manualPreparationProof{tenant: uuid.New(), project: uuid.New(), actor: uuid.New(), session: uuid.New(), binding: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, CanvasHash: strings.Repeat("b", 64)}}
	pin := func(media string, size int64) defectEvidenceObject {
		return defectEvidenceObject{ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}, object: objectstore.Object{Key: uuid.NewString(), VersionID: "original-version", SHA256: strings.Repeat("c", 64), Bytes: size, ContentType: media}}
	}
	proof.objects = defectMaskObjects{Root: pin("application/json", 100), Params: pin("application/json", 100), Pixel: pin("application/octet-stream", 128)}
	proof.children = defectManualOutputs{pin("application/octet-stream", 128), pin("image/png", 100), pin("image/png", 100)}
	return proof
}
