package semanticreview

import (
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func previewFixture(now time.Time) (Preview, PreviewRequest) {
	request := PreviewRequest{ExpectedRevision: "9", TargetRevision: "9007199254740993", PlanSHA: strings.Repeat("a", 64), CandidateSHA: strings.Repeat("b", 64)}
	media := func() PreviewMedia {
		return PreviewMedia{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64), URL: "https://media.example/image", ExpiresAt: now.Add(time.Minute), Width: 1024, Height: 768, MediaType: "image/png"}
	}
	return Preview{SchemaVersion: 1, TenantID: uuid.New(), IntentID: uuid.New(), TargetID: uuid.New(), TargetRevision: request.TargetRevision, AssetVersionID: uuid.New(), WorkflowID: uuid.New(), WorkflowRevision: request.ExpectedRevision, PlanSHA: request.PlanSHA,
		Candidate: TargetDocument{ArtifactID: uuid.New(), SHA256: request.CandidateSHA}, Render: TargetDocument{ArtifactID: uuid.New(), SHA256: strings.Repeat("d", 64)}, Evidence: TargetDocument{ArtifactID: uuid.New(), SHA256: strings.Repeat("e", 64)}, QualityStatus: "review", Limitations: []string{}, ProtectedPixelsUnchanged: true, Before: media(), After: media(), Affected: media(), Protected: media()}, request
}
func TestPreviewTransportFencesAndExpiry(t *testing.T) {
	now := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	valid, request := previewFixture(now)
	if err := valid.Validate(valid.TenantID, valid.IntentID, valid.TargetID, request, now); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"tenant", "target", "candidate", "revision", "plan", "protection", "quality", "dimensions", "alias", "document_alias", "expired", "long_expiry", "url", "overlay"} {
		t.Run(kind, func(t *testing.T) {
			bad := valid
			switch kind {
			case "tenant":
				bad.TenantID = uuid.New()
			case "target":
				bad.TargetID = uuid.New()
			case "candidate":
				bad.Candidate.SHA256 = strings.Repeat("f", 64)
			case "revision":
				bad.WorkflowRevision = "10"
			case "plan":
				bad.PlanSHA = strings.Repeat("f", 64)
			case "protection":
				bad.ProtectedPixelsUnchanged = false
			case "quality":
				bad.QualityStatus = "unsupported"
			case "dimensions":
				bad.After.Width++
			case "alias":
				bad.After.ArtifactID = bad.Before.ArtifactID
			case "document_alias":
				bad.After.ArtifactID = bad.Render.ArtifactID
			case "expired":
				bad.Before.ExpiresAt = now
			case "long_expiry":
				bad.After.ExpiresAt = now.Add(5*time.Minute + time.Nanosecond)
			case "url":
				bad.After.URL = "file:///private/image"
			case "overlay":
				bad.Protected.MediaType = "image/jpeg"
			}
			if err := bad.Validate(valid.TenantID, valid.IntentID, valid.TargetID, request, now); err == nil {
				t.Fatal("unsafe preview accepted")
			}
		})
	}
}
func TestPreviewRequestRejectsInvalidFences(t *testing.T) {
	_, valid := previewFixture(time.Now())
	for _, revision := range []string{"", "0", "01", "-1", "1e2", "9223372036854775807"} {
		bad := valid
		bad.ExpectedRevision = revision
		if bad.Validate() == nil {
			t.Fatal("invalid revision accepted", revision)
		}
	}
}
