package v2draft

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestSemanticTransferPublicationValidatesBeforeDependencies(t *testing.T) {
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	reference := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref, CanvasHash: strings.Repeat("b", 64)}
	target := reference
	target.AssetVersionID = uuid.New()
	params := v2input.SemanticTransferParams{SchemaVersion: 1, Intent: ref, Reference: reference, Target: target, ModelLockSHA: strings.Repeat("c", 64), Calibration: ref}
	raw, err := canonical.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	publisher := Publisher{}
	for _, body := range [][]byte{[]byte(`{}`), []byte(strings.Replace(string(raw), `"schema_version":1`, `"schema_version":2`, 1))} {
		_, err := publisher.PublishSemanticTransferParams(context.Background(), auth.Identity{}, uuid.New(), target.AssetVersionID, uuid.New(), body)
		if err == nil || !strings.Contains(err.Error(), "VALIDATION_FAILED") {
			t.Fatal("invalid params", err)
		}
	}
	_, err = publisher.PublishSemanticTransferParams(context.Background(), auth.Identity{}, uuid.New(), reference.AssetVersionID, uuid.New(), raw)
	if err == nil || !strings.Contains(err.Error(), "VALIDATION_FAILED") {
		t.Fatal("wrong target accepted", err)
	}
	_, err = publisher.PublishSemanticTransferParams(context.Background(), auth.Identity{}, uuid.New(), target.AssetVersionID, uuid.New(), raw)
	if err == nil || !strings.Contains(err.Error(), "DEPENDENCY_UNAVAILABLE") {
		t.Fatal("valid params did not reach publisher", err)
	}
}
