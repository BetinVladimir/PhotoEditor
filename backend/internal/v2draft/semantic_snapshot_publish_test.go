package v2draft

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestSemanticSnapshotPublicationRejectsDigestAndVersionBeforeIO(t *testing.T) {
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	version := uuid.New()
	_, body, err := v2input.CompilePinnedSessionInputSnapshot(uuid.New(), []v2input.SessionSnapshotItem{{Ordinal: 0, Binding: v2input.SessionVersionBinding{AssetVersionID: version, Source: ref, CanvasHash: strings.Repeat("b", 64)}, WorkingImage: ref, Recipe: ref}})
	if err != nil {
		t.Fatal(err)
	}
	publisher := Publisher{}
	for _, test := range []struct {
		version uuid.UUID
		body    []byte
	}{
		{uuid.New(), body},
		{version, []byte(strings.Replace(string(body), strings.Repeat("b", 64), strings.Repeat("c", 64), 1))},
		{version, []byte(`{}`)},
	} {
		_, err := publisher.PublishSemanticSnapshot(context.Background(), auth.Identity{}, uuid.New(), test.version, uuid.New(), test.body)
		if err == nil || !strings.Contains(err.Error(), "VALIDATION_FAILED") {
			t.Fatal("invalid snapshot reached IO", err)
		}
	}
	_, err = publisher.PublishSemanticSnapshot(context.Background(), auth.Identity{}, uuid.New(), version, uuid.New(), body)
	if err == nil || !strings.Contains(err.Error(), "DEPENDENCY_UNAVAILABLE") {
		t.Fatal("valid snapshot did not reach reservation", err)
	}
}
