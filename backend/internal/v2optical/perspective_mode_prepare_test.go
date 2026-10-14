package v2optical

import (
	"context"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestPerspectiveModePrepareManualAndCancellation(t *testing.T) {
	c := PerspectiveCoordinator{MaxPixels: 8000}
	p := perspectiveIntent()
	p.Strength = 0
	got, originals, err := c.prepareGeometry(context.Background(), auth.Identity{}, PerspectivePrepareRequest{}, p, 100, 80)
	if err != nil || got.Width != 100 || got.Height != 80 || originals != nil {
		t.Fatal(got, originals, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := c.prepareGeometry(ctx, auth.Identity{}, PerspectivePrepareRequest{}, p, 100, 80); err != context.Canceled {
		t.Fatal(err)
	}
	p.Mode = "auto"
	p.Corners = nil
	p.Lines = &p.Base.Source
	if _, _, err := c.prepareGeometry(context.Background(), auth.Identity{}, PerspectivePrepareRequest{}, p, 100, 80); err == nil {
		t.Fatal("auto admitted without catalogue authority")
	}
}

func TestPerspectiveAutoGeometryPreparationRequiresLineAuthorityAtZeroStrength(t *testing.T) {
	c := PerspectiveCoordinator{MaxPixels: 8000}
	p := perspectiveIntent()
	p.Mode = "auto"
	p.Corners = nil
	p.Lines = &p.Base.Source
	p.Strength = 0
	if _, _, err := c.prepareGeometry(context.Background(), auth.Identity{}, PerspectivePrepareRequest{}, p, 100, 80); err == nil {
		t.Fatal("zero auto skipped line authority")
	}
}
