package httpapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
)

type reviewPreviewStub struct{ calls int }

func (s *reviewPreviewStub) Preview(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, string) (review.CandidatePreview, error) {
	s.calls++
	return review.CandidatePreview{}, nil
}

func TestReviewPreviewRoutesRequireAuthenticatedSession(t *testing.T) {
	for _, side := range []string{"before", "after"} {
		t.Run(side, func(t *testing.T) {
			provider := &reviewPreviewStub{}
			server := &Server{Auth: &auth.Service{Pepper: []byte("review-preview")}, ReviewPreviews: provider}
			response := httptest.NewRecorder()
			server.Routes().ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/tenants/"+uuid.NewString()+"/reviews/"+uuid.NewString()+"/previews/"+side+"/preview1600", nil))
			if response.Code != 401 || provider.calls != 0 {
				t.Fatal("unauthenticated preview reached provider", response.Code, provider.calls)
			}
		})
	}
}

func TestReviewPreviewPublicResponseBoundary(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	id := uuid.New()
	sha := strings.Repeat("a", 64)
	pin := func() review.ArtifactRef { return review.ArtifactRef{ID: uuid.New(), SHA256: sha} }
	link := func(mime string) review.CandidatePreviewLink {
		return review.CandidatePreviewLink{Artifact: pin(), Bytes: 100, MediaType: mime, URL: "https://media.test/versioned?versionId=v1", ExpiresAt: now.Add(5 * time.Minute)}
	}
	base := review.CandidatePreview{Review: id, Side: "before", Source: pin(), ProfileHash: sha, Width: 1600, Height: 1000, Manifest: link("application/json"), Image: link("image/jpeg")}
	cases := map[string]func(*review.CandidatePreview){
		"foreign_review": func(v *review.CandidatePreview) { v.Review = uuid.New() },
		"foreign_side":   func(v *review.CandidatePreview) { v.Side = "after" },
		"source_sha":     func(v *review.CandidatePreview) { v.Source.SHA256 = "invalid" },
		"profile_sha":    func(v *review.CandidatePreview) { v.ProfileHash = strings.Repeat("A", 64) },
		"image_sha":      func(v *review.CandidatePreview) { v.Image.Artifact.SHA256 = "" },
		"alias":          func(v *review.CandidatePreview) { v.Manifest.Artifact.ID = v.Source.ID },
		"manifest_mime":  func(v *review.CandidatePreview) { v.Manifest.MediaType = "text/html" },
		"image_mime":     func(v *review.CandidatePreview) { v.Image.MediaType = "application/json" },
		"manifest_size":  func(v *review.CandidatePreview) { v.Manifest.Bytes = 1048577 },
		"image_size":     func(v *review.CandidatePreview) { v.Image.Bytes = 52*1048576 + 1 },
		"zero_size":      func(v *review.CandidatePreview) { v.Image.Bytes = 0 },
		"geometry":       func(v *review.CandidatePreview) { v.Width = 1601 },
		"zero_geometry":  func(v *review.CandidatePreview) { v.Height = 0 },
		"expired":        func(v *review.CandidatePreview) { v.Image.ExpiresAt = now },
		"long_expiry":    func(v *review.CandidatePreview) { v.Manifest.ExpiresAt = now.Add(7 * time.Minute) },
		"scheme":         func(v *review.CandidatePreview) { v.Image.URL = "file:///tmp/image" },
		"credentials":    func(v *review.CandidatePreview) { v.Image.URL = "https://user:secret@media.test/image" },
		"fragment":       func(v *review.CandidatePreview) { v.Image.URL += "#fragment" },
		"missing_version": func(v *review.CandidatePreview) { v.Image.URL = "https://media.test/versioned" },
		"duplicate_version": func(v *review.CandidatePreview) { v.Image.URL += "&versionId=v2" },
	}
	if !validReviewPreviewResponse(base, id, "before", "preview1600", now) {
		t.Fatal("valid preview refused")
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			value := base
			mutate(&value)
			if validReviewPreviewResponse(value, id, "before", "preview1600", now) {
				t.Fatal("invalid response accepted")
			}
		})
	}
	if validReviewPreviewResponse(base, id, "before", "unknown", now) {
		t.Fatal("unknown variant accepted")
	}
}
