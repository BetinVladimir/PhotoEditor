package v2portrait

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
)

type restorationSelectionDouble struct {
	value  restorationSelectedSource
	calls  int
	change bool
}

func (c *restorationSelectionDouble) lookup(_ context.Context, _ auth.Identity, _ RestorationDecisionIdentity) (restorationSelectedSource, error) {
	c.calls++
	v := c.value
	if v.Source != nil {
		owned := *v.Source
		v.Source = &owned
		if c.change && c.calls > 1 {
			v.Source.Version = "changed"
		}
	}
	return v, nil
}

type forbiddenRestorationObjects struct{ opens int }

func (o *forbiddenRestorationObjects) Open(context.Context, objectstore.Object) (io.ReadCloser, error) {
	o.opens++
	return nil, nil
}

func TestRestorationSelectedSourceBindsDecisionAndSkip(t *testing.T) {
	id := RestorationDecisionIdentity{Tenant: uuid.New(), Project: uuid.New(), Review: uuid.New(), Task: uuid.New(), Run: uuid.New(), DecisionHash: strings.Repeat("a", 64), Route: "skip"}
	wire := restorationSelectedSource{SchemaVersion: 1, Tenant: id.Tenant, Project: id.Project, Review: id.Review, Task: id.Task, Run: id.Run, DecisionHash: id.DecisionHash, Route: id.Route}
	raw, err := canonical.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decodeRestorationSelectedSource(raw, id, "photos"); err != nil {
		t.Fatal(err)
	}
	changed := id
	changed.Project = uuid.New()
	if _, err = decodeRestorationSelectedSource(raw, changed, "photos"); err == nil {
		t.Fatal("foreign project accepted")
	}
	changed = id
	changed.DecisionHash = strings.Repeat("b", 64)
	if _, err = decodeRestorationSelectedSource(raw, changed, "photos"); err == nil {
		t.Fatal("changed decision accepted")
	}
	reader := &forbiddenRestorationObjects{}
	catalog := &restorationSelectionDouble{value: wire}
	got, err := loadRestorationSelectedSource(context.Background(), catalog, reader, auth.Identity{}, id)
	if err != nil || got.Image() != nil || len(got.WorkingImageBytes()) != 0 || catalog.calls != 2 || reader.opens != 0 {
		t.Fatal("skip opened a source", err)
	}
	wire.Source = &restorationSelectedLocator{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64), Bucket: "photos", Key: "image", Version: "v1", Bytes: 100, MediaType: "application/json"}
	raw, err = canonical.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decodeRestorationSelectedSource(raw, id, "photos"); err == nil {
		t.Fatal("skip carried image")
	}
}

func TestRestorationSelectedSourceRequiresVersionedImage(t *testing.T) {
	id := RestorationDecisionIdentity{Tenant: uuid.New(), Project: uuid.New(), Review: uuid.New(), Task: uuid.New(), Run: uuid.New(), DecisionHash: strings.Repeat("a", 64), Route: "candidate"}
	wire := restorationSelectedSource{SchemaVersion: 1, Tenant: id.Tenant, Project: id.Project, Review: id.Review, Task: id.Task, Run: id.Run, DecisionHash: id.DecisionHash, Route: id.Route, Source: &restorationSelectedLocator{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64), Bucket: "photos", Key: "image", Version: "v1", Bytes: 100, MediaType: "application/json"}}
	raw, err := canonical.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decodeRestorationSelectedSource(raw, id, "photos"); err != nil {
		t.Fatal(err)
	}
	wire.Source.Version = "null"
	raw, _ = canonical.Marshal(wire)
	if _, err = decodeRestorationSelectedSource(raw, id, "photos"); err == nil {
		t.Fatal("unversioned image accepted")
	}
	id.Route = "base"
	if _, err = decodeRestorationSelectedSource(raw, id, "photos"); err == nil {
		t.Fatal("route substitution accepted")
	}
}
