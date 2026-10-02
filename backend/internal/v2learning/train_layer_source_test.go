package v2learning

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type trainingLayerCatalog struct{ object objectstore.Object }

func (c trainingLayerCatalog) Lookup(_ context.Context, _ auth.Identity,
	_ uuid.UUID, node v2draft.LayerRef) (objectstore.Object, error) {
	if node.Contract != "LayerStack" ||
		node.Path != "EditingRecipeV3.layer_stack" ||
		node.Ref.SHA256 != c.object.SHA256 {
		return objectstore.Object{}, ErrInvalidTrainingDraft
	}
	return c.object, nil
}

type trainingLayerObject struct{ body []byte }

func (o trainingLayerObject) Open(_ context.Context,
	_ objectstore.Object) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(o.body)), nil
}

func TestTrainingLayerSourcePinsScopedCanonicalBytes(t *testing.T) {
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(),
		Source: v2input.Ref{ArtifactID: uuid.New(),
			SHA256: strings.Repeat("a", 64)},
		CanvasHash: strings.Repeat("b", 64)}
	body, err := canonical.Marshal(map[string]any{
		"schema_version": 1, "source": binding, "layers": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	ref := v2input.Ref{ArtifactID: uuid.New(),
		SHA256: hex.EncodeToString(sum[:])}
	object := objectstore.Object{Key: "layer", SHA256: ref.SHA256,
		Bytes: int64(len(body)), VersionID: "v1",
		ContentType: "application/json"}
	source := TrainingLayerSource{Catalog: trainingLayerCatalog{object},
		Objects: trainingLayerObject{body}}
	got, active, err := source.Load(context.Background(), auth.Identity{},
		uuid.New(), ref, binding)
	if err != nil || active || !bytes.Equal(got, body) {
		t.Fatalf("scoped layer rejected: %v", err)
	}
	source.Objects = trainingLayerObject{[]byte(`{}`)}
	if _, _, err := source.Load(context.Background(), auth.Identity{},
		uuid.New(), ref, binding); err == nil {
		t.Fatal("changed layer object accepted")
	}
}
