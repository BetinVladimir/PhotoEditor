package v2render

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestRenderSubmissionIdentityFreezesOrderedIntent(t *testing.T) {
	tenant, actor, project := uuid.New(), uuid.New(), uuid.New()
	sha := strings.Repeat("a", 64)
	item := func() RenderSelectionItem {
		return RenderSelectionItem{Binding: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, CanvasHash: sha}, Recipe: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}}
	}
	request := RenderBatchSubmissionRequest{SnapshotID: uuid.New(), PolicySHA: sha, Selection: []RenderSelectionItem{item(), item()}}
	run, digest, err := CompileRenderSubmissionIdentity(tenant, actor, project, "test-render-key", request)
	if err != nil {
		t.Fatal(err)
	}
	repeated, repeatedSHA, err := CompileRenderSubmissionIdentity(tenant, actor, project, "test-render-key", request)
	if err != nil || repeated != run || repeatedSHA != digest {
		t.Fatal("unstable request identity", err)
	}
	changed := request
	changed.Selection = []RenderSelectionItem{request.Selection[1], request.Selection[0]}
	other, otherSHA, err := CompileRenderSubmissionIdentity(tenant, actor, project, "test-render-key", changed)
	if err != nil || other != run || otherSHA == digest {
		t.Fatal("ordered conflict not bound to same key", err)
	}
	changed.PolicySHA = strings.Repeat("b", 64)
	_, policySHA, err := CompileRenderSubmissionIdentity(tenant, actor, project, "test-render-key", changed)
	if err != nil || policySHA == otherSHA {
		t.Fatal("policy omitted from identity", err)
	}
	duplicated := request
	duplicated.Selection = []RenderSelectionItem{request.Selection[0], request.Selection[0]}
	if _, _, err := CompileRenderSubmissionIdentity(tenant, actor, project, "test-render-key", duplicated); err == nil {
		t.Fatal("duplicate version accepted")
	}
	invalid := request
	invalid.Selection = append([]RenderSelectionItem(nil), request.Selection...)
	invalid.Selection[1].Recipe.SHA256 = "bad"
	if _, _, err := CompileRenderSubmissionIdentity(tenant, actor, project, "test-render-key", invalid); err == nil {
		t.Fatal("invalid later frame accepted")
	}
}
