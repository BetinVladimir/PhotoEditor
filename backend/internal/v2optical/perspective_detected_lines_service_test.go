package v2optical

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestDetectedLinesDescriptionOwnsScopeAndRefusesInvalidResult(t *testing.T) {
	tenant, project, version, task, resultID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	detection := DetectedPerspectiveLines{SchemaVersion: 1, Base: v2input.SessionVersionBinding{AssetVersionID: version, Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, CanvasHash: strings.Repeat("b", 64)}, Status: "unsupported"}
	raw, err := canonical.Marshal(detection)
	if err != nil {
		t.Fatal(err)
	}
	sha, err := canonical.Hash(detection)
	if err != nil {
		t.Fatal(err)
	}
	root := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
	proposal, err := describePerspectiveDetectedLines(tenant, project, version, task, resultID, root, raw)
	if err != nil || proposal.Tenant != tenant || proposal.Project != project || proposal.Producer != task || proposal.ResultID != resultID || proposal.Result != root || proposal.Detection.Base != detection.Base {
		t.Fatal("lost proposal binding", err)
	}
	if _, err := describePerspectiveDetectedLines(tenant, project, version, task, uuid.Nil, root, raw); err == nil {
		t.Fatal("zero result accepted")
	}
	if _, err := describePerspectiveDetectedLines(tenant, project, uuid.New(), task, resultID, root, raw); err == nil {
		t.Fatal("foreign version accepted")
	}
	root.SHA256 = strings.Repeat("c", 64)
	if _, err := describePerspectiveDetectedLines(tenant, project, version, task, resultID, root, raw); err == nil {
		t.Fatal("changed hash accepted")
	}
}
func TestDetectedLinesDescribeCancellationAndMissingDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (PerspectiveDetectedLinesService{}).Describe(ctx, auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err := (PerspectiveDetectedLinesService{}).Describe(context.Background(), auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil); err == nil {
		t.Fatal("missing DB accepted")
	}
}
