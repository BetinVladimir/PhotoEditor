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

type selectedMediaProvider struct {
	reads int
	body  []byte
	mime  string
}

func (*selectedMediaProvider) Preview(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, string) (review.CandidatePreview, error) {
	return review.CandidatePreview{}, nil
}
func (*selectedMediaProvider) IssueSelectedRenderPreview(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, []byte, string) (review.CandidatePreview, error) {
	return review.CandidatePreview{}, nil
}
func (p *selectedMediaProvider) ReadSelectedRenderPreview(_ context.Context, _ []byte, _ string, _ time.Time) ([]byte, string, error) {
	p.reads++
	if p.mime != "" || p.body != nil {
		return p.body, p.mime, nil
	}
	return []byte("image-bytes"), "image/jpeg", nil
}

func TestSelectedRenderMediaRequiresExactCapabilityAndVersion(t *testing.T) {
	now := time.Now()
	key := []byte(strings.Repeat("k", 32))
	grant := review.SelectedPreviewGrant{Version: 1, Tenant: uuid.New(), Review: uuid.New(), User: uuid.New(), Session: uuid.New(), Kind: "render",
		DecisionHash: strings.Repeat("a", 64), Route: "candidate", Variant: "preview1600", Role: "image",
		Artifact: uuid.New(), SHA256: strings.Repeat("b", 64), ObjectKeySHA: strings.Repeat("c", 64), ObjectVersion: "v1", ExpiresUnix: now.Add(2 * time.Minute).Unix()}
	token, err := review.SealSelectedPreviewGrant(key, grant, now)
	if err != nil {
		t.Fatal(err)
	}
	provider := new(selectedMediaProvider)
	server := &Server{Auth: &auth.Service{Pepper: key}, SelectedPreviewKey: key, ReviewPreviews: provider}
	base := "/api/v1/media/render-selected?token=" + token + "&versionId="
	for _, tc := range []struct {
		url  string
		want int
	}{
		{base + "v1", 200},
		{base + "v2", 404},
		{base + "v1&versionId=v2", 404},
		{base + "v1&other=1", 404},
		{"/api/v1/media/render-selected?versionId=v1", 404},
	} {
		response := httptest.NewRecorder()
		server.Routes().ServeHTTP(response, httptest.NewRequest("GET", tc.url, nil))
		if response.Code != tc.want || response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("selected media boundary", tc.url, response.Code, response.Header())
		}
		if tc.want == 200 && response.Header().Get("Content-Length") != "11" {
			t.Fatal("selected preview did not expose verified byte length", response.Header())
		}
	}
	if provider.reads != 1 {
		t.Fatal("invalid capability reached physical reader", provider.reads)
	}
	restoration := grant
	restoration.Kind = "restoration"
	restoredToken, err := review.SealSelectedPreviewGrant(key, restoration, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/api/v1/media/restoration-selected", 200},
		{"/api/v1/media/render-selected", 404},
	} {
		response := httptest.NewRecorder()
		server.Routes().ServeHTTP(response, httptest.NewRequest("GET", tc.path+"?token="+restoredToken+"&versionId=v1", nil))
		if response.Code != tc.want {
			t.Fatal("selected capability kind crossed route", tc.path, response.Code)
		}
	}
}

func TestSelectedMediaRefusesProviderBodyWithWrongType(t *testing.T) {
	now := time.Now()
	key := []byte(strings.Repeat("k", 32))
	grant := review.SelectedPreviewGrant{Version: 1, Tenant: uuid.New(), Review: uuid.New(), User: uuid.New(), Session: uuid.New(), Kind: "render",
		DecisionHash: strings.Repeat("a", 64), Route: "candidate", Variant: "preview1600", Role: "image",
		Artifact: uuid.New(), SHA256: strings.Repeat("b", 64), ObjectKeySHA: strings.Repeat("c", 64), ObjectVersion: "v1", ExpiresUnix: now.Add(time.Minute).Unix()}
	token, err := review.SealSelectedPreviewGrant(key, grant, now)
	if err != nil {
		t.Fatal(err)
	}
	provider := &selectedMediaProvider{body: []byte("<script>bad</script>"), mime: "text/html"}
	server := &Server{Auth: &auth.Service{Pepper: key}, SelectedPreviewKey: key, ReviewPreviews: provider}
	response := httptest.NewRecorder()
	server.Routes().ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/media/render-selected?token="+token+"&versionId=v1", nil))
	if response.Code != 503 || provider.reads != 1 || strings.Contains(response.Body.String(), "script") {
		t.Fatal("provider returned active content through media route", response.Code, response.Body.String())
	}
}
