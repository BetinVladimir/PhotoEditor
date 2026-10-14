package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestAdoptedPerspectiveBindingExactProducerSource(t *testing.T) {
	version := uuid.New()
	root := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	binding := AdoptedPerspectiveBinding{ResultID: uuid.New(), Result: root, Base: v2input.SessionVersionBinding{AssetVersionID: version, Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}, CanvasHash: strings.Repeat("c", 64)}}
	if err := validateAdoptedPerspectiveBinding(binding, root, version); err != nil {
		t.Fatal(err)
	}
	changed := binding
	changed.ResultID = uuid.Nil
	if err := validateAdoptedPerspectiveBinding(changed, root, version); err == nil {
		t.Fatal("missing result accepted")
	}
	changed = binding
	changed.Base.CanvasHash = "invalid"
	if err := validateAdoptedPerspectiveBinding(changed, root, version); err == nil {
		t.Fatal("invalid canvas accepted")
	}
	if err := validateAdoptedPerspectiveBinding(binding, root, uuid.New()); err == nil {
		t.Fatal("foreign version accepted")
	}
	if _, _, err := LoadAdoptedPerspectiveResult(context.Background(), nil, nil, auth.Identity{}, uuid.New(), uuid.New(), version, uuid.New(), root, "photos"); err == nil {
		t.Fatal("missing object authority accepted")
	}
}
