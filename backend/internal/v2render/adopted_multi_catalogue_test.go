package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type multiCataloguePinFixture struct {
	*multiJSONChildSource
	*multiOriginalInventoryTestSource
}

func TestMultiCataloguePinsRootAndEachChildLocator(t *testing.T) {
	for _, change := range []string{"none", "root_version", "binding", "child_version", "child_bytes"} {
		t.Run(change, func(t *testing.T) {
			b, task, child, _, ref := multiJSONChildFixture(t)
			source := &multiCataloguePinFixture{multiJSONChildSource: child, multiOriginalInventoryTestSource: &multiOriginalInventoryTestSource{}}
			pin := &pinnedMultiCatalogueSource{AdoptedMultiCatalogueSource: source}
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			tenant, project := uuid.New(), uuid.New()
			if _, _, err := pin.LoadObject(context.Background(), who, tenant, project, task, b.Operation, b.Result, "photos"); err != nil {
				t.Fatal(err)
			}
			if _, err := pin.LoadChildObject(context.Background(), who, tenant, project, task, b.Operation, b.Result, ref, "$/report", "ImageReport", "photos"); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "root_version":
				child.object.VersionID = "replacement"
			case "binding":
				child.binding.Snapshot = uuid.New()
			case "child_version":
				child.child.VersionID = "replacement"
			case "child_bytes":
				child.child.Bytes++
			}
			var err error
			if change == "child_version" || change == "child_bytes" {
				_, err = pin.LoadChildObject(context.Background(), who, tenant, project, task, b.Operation, b.Result, ref, "$/report", "ImageReport", "photos")
			} else {
				_, _, err = pin.LoadObject(context.Background(), who, tenant, project, task, b.Operation, b.Result, "photos")
			}
			if change == "none" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, v2input.ErrExpired) {
				t.Fatalf("mutation accepted: %v", err)
			}
			if len(pin.children) != 1 {
				t.Fatal("repeated child duplicated")
			}
		})
	}
}
func TestMultiCatalogueZeroAndInvalidPreflight(t *testing.T) {
	var docs MultiResultDocuments
	if _, err := docs.Binding(); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("zero binding")
	}
	if _, err := docs.Image(); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("zero image")
	}
	if _, err := docs.Mask(); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("zero mask")
	}
	if _, err := docs.Report(); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("zero report")
	}
	if _, err := LoadAdoptedMultiDocuments(context.Background(), nil, nil, auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil, "", v2input.Ref{}, ""); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("invalid loader")
	}
}
