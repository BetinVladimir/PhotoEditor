package v2portrait

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
)

type restorationArrayCatalogDouble struct {
	object objectstore.Object
	calls  int
	change bool
}

func (c *restorationArrayCatalogDouble) lookupArray(_ context.Context, _ auth.Identity, _ RestorationDecisionIdentity, _ []byte, _ string, _ review.ArtifactRef) (objectstore.Object, error) {
	c.calls++
	out := c.object
	if c.change && c.calls > 1 {
		out.VersionID = "changed"
	}
	return out, nil
}

type restorationArrayObjectReader struct {
	raw   []byte
	opens int
}

func (r *restorationArrayObjectReader) Open(_ context.Context, _ objectstore.Object) (io.ReadCloser, error) {
	r.opens++
	return io.NopCloser(bytes.NewReader(r.raw)), nil
}

func TestRestorationSelectedArraysVerifyPhysicalPixelsAndRecheck(t *testing.T) {
	_, object, npy := analysisArrayFixture(t, 3, []float32{0, 0.5, 1})
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if err = json.Unmarshal(fixture, &images); err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	image.Canvas.Width, image.Canvas.Height = 1, 1
	sum := sha256.Sum256(npy)
	image.Pixels = review.ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	body, err := canonical.Marshal(image)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = review.ParseWorkingImage(body); err != nil {
		t.Fatal(err)
	}
	docSHA := sha256.Sum256(body)
	id := RestorationDecisionIdentity{Tenant: uuid.New(), Project: uuid.New(), Review: uuid.New(), Task: uuid.New(), Run: uuid.New(), DecisionHash: strings.Repeat("a", 64), Route: "candidate"}
	source := restorationSelectedSource{SchemaVersion: 1, Tenant: id.Tenant, Project: id.Project, Review: id.Review, Task: id.Task, Run: id.Run, DecisionHash: id.DecisionHash, Route: id.Route,
		Source: &restorationSelectedLocator{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(docSHA[:]), Bucket: "photos", Key: "selected/image", Version: "v1", Bytes: int64(len(body)), MediaType: "application/json"}}
	selected := VerifiedRestorationSelection{source: source, raw: string(body)}
	for _, changed := range []bool{false, true} {
		catalog := &restorationArrayCatalogDouble{object: object, change: changed}
		reader := &restorationArrayObjectReader{raw: npy}
		proof, err := VerifyRestorationSelectedArrays(context.Background(), catalog, reader, &restorationSelectionDouble{value: source}, auth.Identity{}, selected)
		if changed {
			if err == nil {
				t.Fatal("changed array version accepted")
			}
		} else if err != nil || proof.Selection().Image() == nil || reader.opens != 1 || catalog.calls != 2 {
			t.Fatal("physical selection not verified", err)
		}
	}
}

func TestRestorationSelectedArrayLocatorBindsExactObject(t *testing.T) {
	ref := review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	value := restorationSelectedLocator{ArtifactID: ref.ID, SHA256: ref.SHA256, Bucket: "photos", Key: "selected/pixels.npy", Version: "v1", Bytes: 100, MediaType: "application/x-npy"}
	raw, err := canonical.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	object, err := decodeRestorationSelectedArray(raw, ref, "photos")
	if err != nil || object.Key != value.Key || object.VersionID != value.Version || object.SHA256 != ref.SHA256 {
		t.Fatal("array locator changed", err)
	}
	for _, mutate := range []func(*restorationSelectedLocator){
		func(v *restorationSelectedLocator) { v.ArtifactID = uuid.New() },
		func(v *restorationSelectedLocator) { v.Version = "null" },
		func(v *restorationSelectedLocator) { v.Bucket = "foreign" },
		func(v *restorationSelectedLocator) { v.MediaType = "application/json" },
	} {
		changed := value
		mutate(&changed)
		body, err := canonical.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = decodeRestorationSelectedArray(body, ref, "photos"); err == nil {
			t.Fatal("substituted array accepted", changed)
		}
	}
}
