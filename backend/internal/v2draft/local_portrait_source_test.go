package v2draft

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestLocalPortraitSourceCurrentScopeAndSchema(t *testing.T) {
	fixtureRaw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if err = json.Unmarshal(fixtureRaw, &images); err != nil {
		t.Fatal(err)
	}
	raw, err := canonical.Bytes(images[0])
	if err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(raw)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	sum := sha256.Sum256(raw)
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: id, SHA256: hex.EncodeToString(sum[:])}, CanvasHash: hash}
	object := objectstore.Object{Key: id.String(), SHA256: binding.Source.SHA256, Bytes: int64(len(raw)), VersionID: "v1", ContentType: "application/json"}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant := uuid.New()
	for _, mode := range []string{"valid", "revoked", "version", "wrong_canvas", "wrong_schema"} {
		objects := closureFixture{objects: map[uuid.UUID]objectstore.Object{id: object}, bodies: map[string][]byte{object.Key: raw}}
		catalog := &changingClosureCatalog{closureFixture: objects, changeAt: 2}
		base := binding
		switch mode {
		case "revoked":
			catalog.deny = v2input.ErrExpired
		case "version":
			catalog.change = func(o *objectstore.Object) { o.VersionID = "v2" }
		case "wrong_canvas":
			base.CanvasHash = strings.Repeat("0", 64)
		case "wrong_schema":
			bad := []byte("{}")
			digest := sha256.Sum256(bad)
			base.Source.SHA256 = hex.EncodeToString(digest[:])
			badObject := object
			badObject.SHA256 = base.Source.SHA256
			badObject.Bytes = int64(len(bad))
			objects.objects[id] = badObject
			objects.bodies[object.Key] = bad
		}
		result, err := (ClosureResolver{Catalog: catalog, Objects: objects}).ReadLocalPortraitSource(context.Background(), who, tenant, base)
		if mode == "valid" {
			if err != nil || string(result) != string(raw) || catalog.calls != 2 {
				t.Fatal("valid source failed", err)
			}
		} else if err == nil || result != nil {
			t.Fatal("unsafe source returned", mode)
		}
	}
}
