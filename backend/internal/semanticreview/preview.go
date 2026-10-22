package semanticreview

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// PreviewRequest is a read fence, not permission to render or apply an edit.
// A provider must independently resolve the current plan and canonical preview
// producer under the authenticated editor's tenant/project authority.
type PreviewRequest struct {
	ExpectedRevision string
	TargetRevision   string
	PlanSHA          string
	CandidateSHA     string
}

func previewRevision(value string) bool {
	if value == "" || len(value) > 19 || value[0] == '0' {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return false
		}
	}
	n, err := strconv.ParseInt(value, 10, 64)
	return err == nil && n > 0 && n < 9223372036854775807
}
func (p PreviewRequest) Validate() error {
	if !previewRevision(p.ExpectedRevision) || !previewRevision(p.TargetRevision) || !shaPattern.MatchString(p.PlanSHA) || !shaPattern.MatchString(p.CandidateSHA) {
		return ErrInvalid
	}
	return nil
}

type PreviewMedia struct {
	ArtifactID uuid.UUID `json:"artifact_id"`
	SHA256     string    `json:"sha256"`
	URL        string    `json:"url"`
	ExpiresAt  time.Time `json:"expires_at"`
	Width      int       `json:"width"`
	Height     int       `json:"height"`
	MediaType  string    `json:"media_type"`
}

type Preview struct {
	SchemaVersion            int            `json:"schema_version"`
	TenantID                 uuid.UUID      `json:"tenant_id"`
	IntentID                 uuid.UUID      `json:"intent_id"`
	TargetID                 uuid.UUID      `json:"target_id"`
	TargetRevision           string         `json:"target_revision"`
	AssetVersionID           uuid.UUID      `json:"asset_version_id"`
	WorkflowID               uuid.UUID      `json:"workflow_id"`
	WorkflowRevision         string         `json:"workflow_revision"`
	PlanSHA                  string         `json:"plan_sha"`
	Candidate                TargetDocument `json:"candidate"`
	Render                   TargetDocument `json:"render"`
	Evidence                 TargetDocument `json:"evidence"`
	QualityStatus            string         `json:"quality_status"`
	Limitations              []string       `json:"limitations"`
	ProtectedPixelsUnchanged bool           `json:"protected_pixels_unchanged"`
	Before                   PreviewMedia   `json:"before"`
	After                    PreviewMedia   `json:"after"`
	Affected                 PreviewMedia   `json:"affected"`
	Protected                PreviewMedia   `json:"protected"`
}

// Validate checks transport scope and lifetimes. It is deliberately not a
// producer/evidence verifier: providers must verify canonical result, object
// versions, render/evidence dependency closure and protected pixels before
// constructing this DTO. A valid DTO alone never authorizes Apply.
func (p Preview) Validate(tenant, intent, target uuid.UUID, request PreviewRequest, now time.Time) error {
	if request.Validate() != nil || tenant == uuid.Nil || intent == uuid.Nil || target == uuid.Nil || now.IsZero() ||
		p.SchemaVersion != 1 || p.TenantID != tenant || p.IntentID != intent || p.TargetID != target ||
		p.AssetVersionID == uuid.Nil || p.WorkflowID == uuid.Nil || p.TargetRevision != request.TargetRevision ||
		p.WorkflowRevision != request.ExpectedRevision || p.PlanSHA != request.PlanSHA ||
		p.Candidate.SHA256 != request.CandidateSHA || !p.ProtectedPixelsUnchanged ||
		(p.QualityStatus != "pass" && p.QualityStatus != "review") || p.Limitations == nil || len(p.Limitations) > 20 {
		return ErrInvalid
	}
	for _, ref := range []TargetDocument{p.Candidate, p.Render, p.Evidence} {
		if ref.ArtifactID == uuid.Nil || !shaPattern.MatchString(ref.SHA256) {
			return ErrInvalid
		}
	}
	if p.Render.ArtifactID == p.Candidate.ArtifactID || p.Evidence.ArtifactID == p.Candidate.ArtifactID || p.Evidence.ArtifactID == p.Render.ArtifactID {
		return ErrInvalid
	}
	for _, value := range p.Limitations {
		if strings.TrimSpace(value) == "" || len([]rune(value)) > 500 {
			return ErrInvalid
		}
	}
	seen := map[uuid.UUID]bool{p.Candidate.ArtifactID: true, p.Render.ArtifactID: true, p.Evidence.ArtifactID: true}
	for i, media := range []PreviewMedia{p.Before, p.After, p.Affected, p.Protected} {
		if media.ArtifactID == uuid.Nil || seen[media.ArtifactID] || !shaPattern.MatchString(media.SHA256) ||
			media.Width < 1 || media.Width > 4096 || media.Height < 1 || media.Height > 4096 ||
			media.Width != p.Before.Width || media.Height != p.Before.Height ||
			!media.ExpiresAt.After(now) || media.ExpiresAt.After(now.Add(5*time.Minute)) ||
			(media.MediaType != "image/png" && media.MediaType != "image/jpeg" && media.MediaType != "image/webp") ||
			(i >= 2 && media.MediaType != "image/png") || !previewURL(media.URL) {
			return ErrInvalid
		}
		seen[media.ArtifactID] = true
	}
	return nil
}
func previewURL(value string) bool {
	if len(value) < 1 || len(value) > 8192 {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	host := parsed.Hostname()
	return parsed.Scheme == "http" && (host == "localhost" || host == "127.0.0.1" || host == "::1")
}
