package semanticreview

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func previewParamsFixture() ExposurePreviewParams {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	return ExposurePreviewParams{SchemaVersion: 1, TenantID: uuid.New(), IntentID: uuid.New(), TargetID: uuid.New(),
		WorkflowID: uuid.New(), WorkflowRevision: "9007199254740993", TargetRevision: "9223372036854775806", SnapshotID: uuid.New(),
		Plan: ref(), Intent: ref(), Candidate: ref(), Protection: ref(), Source: v2input.SessionVersionBinding{
			AssetVersionID: uuid.New(), Source: ref(), CanvasHash: strings.Repeat("b", 64)},
		ModelLockSHA: strings.Repeat("c", 64), RuntimeLockSHA: strings.Repeat("d", 64), DeviceProfileSHA: strings.Repeat("e", 64)}
}
func TestFreezePreviewPreservesDecimalRevisionAndHasNoApproval(t *testing.T) {
	p := previewParamsFixture()
	body, digest, err := FreezeExposurePreviewParams(p)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	if digest != hex.EncodeToString(sum[:]) {
		t.Fatal("digest mismatch")
	}
	var value map[string]any
	if json.Unmarshal(body, &value) != nil {
		t.Fatal("invalid JSON")
	}
	if len(value) != 16 || value["workflow_revision"] != p.WorkflowRevision || value["target_revision"] != p.TargetRevision {
		t.Fatal("worker contract mismatch")
	}
	for _, key := range []string{"approval_id", "child_run_id", "decision"} {
		if _, ok := value[key]; ok {
			t.Fatal("preview borrowed approval")
		}
	}
}
func TestFreezePreviewRejectsInvalidBindingsWithoutPartialDocument(t *testing.T) {
	for _, mutate := range []func(*ExposurePreviewParams){
		func(p *ExposurePreviewParams) { p.TenantID = uuid.Nil },
		func(p *ExposurePreviewParams) { p.WorkflowRevision = "01" },
		func(p *ExposurePreviewParams) { p.TargetRevision = "9223372036854775807" },
		func(p *ExposurePreviewParams) { p.Protection = p.Candidate },
		func(p *ExposurePreviewParams) { p.Source.Source.ArtifactID = uuid.Nil },
		func(p *ExposurePreviewParams) { p.ModelLockSHA = strings.Repeat("A", 64) },
		func(p *ExposurePreviewParams) { p.SchemaVersion = 2 },
	} {
		p := previewParamsFixture()
		mutate(&p)
		body, digest, err := FreezeExposurePreviewParams(p)
		if err == nil || body != nil || digest != "" {
			t.Fatal("invalid preview frozen")
		}
	}
}
