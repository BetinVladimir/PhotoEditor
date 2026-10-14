package v2optical

import (
	"context"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"testing"
)

func TestPerspectiveLinesDraftDeterministicAndOwned(t *testing.T) {
	source, err := os.ReadFile("testdata/negative-source.json")
	if err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(source)
	if err != nil {
		t.Fatal(err)
	}
	source, err = canonical.Marshal(image)
	if err != nil {
		t.Fatal(err)
	}
	base := perspectiveIntent().Base
	base.Source.SHA256, err = canonical.Hash(image)
	if err != nil {
		t.Fatal(err)
	}
	base.CanvasHash, err = canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant, project, snapshot := uuid.New(), uuid.New(), uuid.New()
	lines := PerspectiveLineSegments{SchemaVersion: 1, Lines: []PerspectiveLine{
		{A: PerspectivePoint{0, .2}, B: PerspectivePoint{1, .2}, Family: "horizontal"}, {A: PerspectivePoint{0, .8}, B: PerspectivePoint{1, .8}, Family: "horizontal"},
		{A: PerspectivePoint{.2, 0}, B: PerspectivePoint{.2, 1}, Family: "vertical"}, {A: PerspectivePoint{.8, 0}, B: PerspectivePoint{.8, 1}, Family: "vertical"}}}
	first, err := CompilePerspectiveLinesDraft(context.Background(), who, tenant, project, snapshot, "lines-draft-key", base, source, lines)
	if err != nil {
		t.Fatal(err)
	}
	again, err := CompilePerspectiveLinesDraft(context.Background(), who, tenant, project, snapshot, "lines-draft-key", base, source, lines)
	if err != nil || first.raw != again.raw {
		t.Fatal(err)
	}
	saved, err := first.Snapshot()
	if err != nil || saved.Bytes != int64(len(saved.LinesJCS)) {
		t.Fatal(saved, err)
	}
	source[0] = '!'
	lines.Lines[0].Family = "invalid"
	owned, err := first.Snapshot()
	if err != nil || owned.SourceJCS != saved.SourceJCS || owned.LinesJCS != saved.LinesJCS {
		t.Fatal("caller changed draft", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CompilePerspectiveLinesDraft(ctx, who, tenant, project, snapshot, "lines-draft-key", base, source, lines); err != context.Canceled {
		t.Fatal(err)
	}
}

func TestSavePerspectiveLinesDraftInvalidBeforeDatabase(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	if err := SavePerspectiveLinesDraft(context.Background(), nil, who, uuid.New(), PerspectiveLinesDraft{}); err == nil {
		t.Fatal("empty draft accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := SavePerspectiveLinesDraft(ctx, nil, who, uuid.New(), PerspectiveLinesDraft{}); err != context.Canceled {
		t.Fatal(err)
	}
}
