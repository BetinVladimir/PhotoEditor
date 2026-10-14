package v2optical

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestPerspectiveLinesIntentStrictSchema(t *testing.T) {
	intent := PerspectiveLinesIntent{SchemaVersion: 1, Snapshot: uuid.New(), Base: perspectiveIntent().Base, Lines: PerspectiveLineSegments{SchemaVersion: 1, Lines: []PerspectiveLine{
		{A: PerspectivePoint{0, .2}, B: PerspectivePoint{1, .2}, Family: "horizontal"}, {A: PerspectivePoint{0, .8}, B: PerspectivePoint{1, .8}, Family: "horizontal"},
		{A: PerspectivePoint{.2, 0}, B: PerspectivePoint{.2, 1}, Family: "vertical"}, {A: PerspectivePoint{.8, 0}, B: PerspectivePoint{.8, 1}, Family: "vertical"}}}}
	raw, err := canonical.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodePerspectiveLinesIntent(raw); err != nil {
		t.Fatal(err)
	}
	altered := append(append([]byte(nil), raw[:len(raw)-1]...), []byte(`,"object_key":"foreign"}`)...)
	if _, err := DecodePerspectiveLinesIntent(altered); err == nil {
		t.Fatal("authority override accepted")
	}
	var service *PerspectiveService
	if _, err := service.PublishProjectLines(context.Background(), auth.Identity{}, uuid.New(), uuid.New(), "lines-key", intent); err == nil {
		t.Fatal("missing service accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.PublishProjectLines(ctx, auth.Identity{}, uuid.New(), uuid.New(), "lines-key", intent); err != context.Canceled {
		t.Fatal(err)
	}
}
