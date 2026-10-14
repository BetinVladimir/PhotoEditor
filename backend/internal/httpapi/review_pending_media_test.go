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

type pendingMediaProvider struct{ reads int }

func (*pendingMediaProvider) Preview(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, string) (review.CandidatePreview, error) {
	return review.CandidatePreview{}, nil
}
func (*pendingMediaProvider) IssuePendingPreview(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, string, []byte, string) (review.CandidatePreview, error) {
	return review.CandidatePreview{}, nil
}
func (p *pendingMediaProvider) ReadPendingPreview(context.Context, []byte, string, time.Time) ([]byte, string, error) {
	p.reads++
	return []byte("image-bytes"), "image/jpeg", nil
}

func TestPendingReviewMediaRequiresExactTokenAndVersion(t *testing.T) {
	now := time.Now()
	key := []byte(strings.Repeat("k", 32))
	grant := review.PendingPreviewGrant{Version: 1, Tenant: uuid.New(), Review: uuid.New(), User: uuid.New(), Session: uuid.New(),
		Side: "after", Variant: "preview1600", Source: uuid.New(), SourceSHA: strings.Repeat("a", 64),
		Role: "image", Artifact: uuid.New(), SHA256: strings.Repeat("b", 64), ObjectKeySHA: strings.Repeat("c", 64), ObjectVersion: "v1", ExpiresUnix: now.Add(time.Minute).Unix()}
	token, err := review.SealPendingPreviewGrant(key, grant, now)
	if err != nil {
		t.Fatal(err)
	}
	provider := new(pendingMediaProvider)
	server := &Server{Auth: &auth.Service{Pepper: key}, SelectedPreviewKey: key, ReviewPreviews: provider}
	base := "/api/v1/media/review-pending?token=" + token + "&versionId="
	for _, tc := range []struct {
		url    string
		status int
	}{
		{base + "v1", 200},
		{base + "v2", 404},
		{base + "v1&versionId=v2", 404},
		{base + "v1&other=1", 404},
	} {
		response := httptest.NewRecorder()
		server.Routes().ServeHTTP(response, httptest.NewRequest("GET", tc.url, nil))
		if response.Code != tc.status || response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("pending preview route boundary", tc.url, response.Code)
		}
		if tc.status == 200 && response.Header().Get("Content-Length") != "11" {
			t.Fatal("pending preview did not expose verified byte length", response.Header())
		}
	}
	if provider.reads != 1 {
		t.Fatal("invalid pending token reached reader", provider.reads)
	}
}
