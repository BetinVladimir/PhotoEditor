package v2learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2observation"
)

func TestTrainingFacesSourceRequiresSourceBoundDocument(t *testing.T) {
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(),
		Source: v2input.Ref{ArtifactID: uuid.New(),
			SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		CanvasHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	evidence := v2observation.Evidence{SchemaVersion: 1,
		Binding: v2observation.Binding{AssetVersionID: binding.AssetVersionID,
			Source: v2observation.ArtifactRef{ArtifactID: binding.Source.ArtifactID,
				SHA256: binding.Source.SHA256}, CanvasHash: binding.CanvasHash},
		Faces: []v2observation.Face{}}
	body, err := canonical.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	store := trainingRecipeStore{objects: map[uuid.UUID]objectstore.Object{
		ref.ArtifactID: {Key: "faces", SHA256: ref.SHA256,
			Bytes: int64(len(body)), VersionID: "v1",
			ContentType: "application/json"}},
		bodies: map[string][]byte{"faces": body}}
	source := TrainingFacesSource{Catalog: store, Objects: store}
	count, err := source.Load(context.Background(), auth.Identity{},
		uuid.New(), binding, ref)
	if err != nil || count != 0 {
		t.Fatalf("empty face document rejected: %v", err)
	}
	other := binding
	other.AssetVersionID = uuid.New()
	if _, err := source.Load(context.Background(), auth.Identity{},
		uuid.New(), other, ref); err == nil {
		t.Fatal("foreign face binding accepted")
	}
}
