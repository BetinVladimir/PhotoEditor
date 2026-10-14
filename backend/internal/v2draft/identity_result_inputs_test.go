package v2draft

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type identityInputCatalogFixture struct {
	items  map[string]objectstore.Object
	reads  map[string]int
	rotate bool
}

func (c *identityInputCatalogFixture) ResolveIdentityResultInput(_ context.Context,
	_ IdentityResultScope, role string, ref v2input.Ref) (objectstore.Object, error) {
	object, ok := c.items[role]
	if !ok || object.SHA256 != ref.SHA256 {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	c.reads[role]++
	if c.rotate && role == "after" && c.reads[role] > 2 && c.reads[role]%2 == 0 {
		object.VersionID = "v2"
	}
	return object, nil
}

func TestIdentityResultInputsDeriveAfterFromExactParams(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(fixture, &images) != nil || len(images) == 0 {
		t.Fatal("working image fixture")
	}
	afterRaw := images[0]
	after, err := review.ParseWorkingImage(afterRaw)
	if err != nil {
		t.Fatal(err)
	}
	ref := func(raw []byte) v2input.Ref {
		sum := sha256.Sum256(raw)
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	}
	afterRef := ref(afterRaw)
	canvasHash, err := canonical.Hash(after.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	params := v2input.IdentityCompareParams{SchemaVersion: 1,
		Before:        v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, CanvasHash: canvasHash},
		After:         v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: afterRef, CanvasHash: canvasHash},
		EffectiveMask: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)},
		Protected:     v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)},
		ModelLockSHA:  strings.Repeat("d", 64),
		Calibration:   v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("e", 64)},
		Consents:      v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("f", 64)}}
	paramsRaw, _, err := v2input.CompileIdentityCompareParams(params)
	if err != nil {
		t.Fatal(err)
	}
	paramsRef := ref(paramsRaw)
	scope := IdentityResultScope{Tenant: uuid.New(), Task: uuid.New(), Result: uuid.New(), Root: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("1", 64)}}
	catalog := &identityInputCatalogFixture{items: map[string]objectstore.Object{}, reads: map[string]int{}}
	objects := restorationArrayReaderFixture{}
	for _, entry := range []struct {
		role string
		ref  v2input.Ref
		raw  []byte
	}{
		{"params", paramsRef, paramsRaw}, {"after", afterRef, afterRaw}} {
		catalog.items[entry.role] = objectstore.Object{Key: entry.role, VersionID: "v1", SHA256: entry.ref.SHA256, Bytes: int64(len(entry.raw)), ContentType: "application/json"}
		objects[entry.role] = entry.raw
	}
	got, err := LoadIdentityResultInputs(context.Background(), scope, catalog, objects, paramsRef)
	if err != nil || got.AfterRef != afterRef || got.ParamsRef != paramsRef || got.Params.After.Source != afterRef {
		t.Fatal(got, err)
	}
	catalog.rotate = true
	if _, err = LoadIdentityResultInputs(context.Background(), scope, catalog, objects, paramsRef); err == nil {
		t.Fatal("rotated after source accepted")
	}
	catalog.rotate = false
	objects["after"] = append([]byte(nil), afterRaw...)
	objects["after"][len(afterRaw)-1] ^= 1
	if _, err = LoadIdentityResultInputs(context.Background(), scope, catalog, objects, paramsRef); err == nil {
		t.Fatal("changed after bytes accepted")
	}
}
