package httpapi

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/interchange"
)

type interchangeMaskCandidateStub struct {
	calls int
	bad   bool
}

func (s *interchangeMaskCandidateStub) ListEntries(_ context.Context, _ auth.Identity, _, _, _ uuid.UUID) ([]interchange.ImportPersonMaskCandidateEntry, error) {
	s.calls++
	return []interchange.ImportPersonMaskCandidateEntry{}, nil
}

func (s *interchangeMaskCandidateStub) RenderPreview(_ context.Context, _ auth.Identity, _, _, _, _, _, _ uuid.UUID) ([]byte, error) {
	s.calls++
	if s.bad {
		return []byte("invalid"), nil
	}
	return []byte("\x89PNG\r\n\x1a\n"), nil
}

func (s *interchangeMaskCandidateStub) ReadEntry(_ context.Context, _ auth.Identity, _, _, _, entry uuid.UUID) ([]interchange.ImportPersonMaskCandidate, error) {
	s.calls++
	if s.bad {
		entry = uuid.New()
	}
	return []interchange.ImportPersonMaskCandidate{{Decision: interchange.ImportPersonMaskDecisionRequest{
		Action: "grant", EntryID: entry, TargetVersionID: uuid.New(), OverrideArtifactID: uuid.New(), MaskArtifactID: uuid.New(),
	}}}, nil
}

func TestInterchangeMaskCandidatesHTTPChecksEntryBinding(t *testing.T) {
	id, entry := uuid.New(), uuid.New()
	for _, bad := range []bool{false, true} {
		reader := &interchangeMaskCandidateStub{bad: bad}
		response := httptest.NewRecorder()
		serveInterchangeMaskCandidates(response, httptest.NewRequest("GET", "/", nil), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, id, id, id, entry, reader)
		expected := 200
		if bad {
			expected = 503
		}
		if response.Code != expected || reader.calls != 1 {
			t.Fatalf("status=%d calls=%d", response.Code, reader.calls)
		}
	}
}

func TestInterchangeMaskCandidatesRouteRequiresSession(t *testing.T) {
	server := &Server{Auth: &auth.Service{Pepper: []byte("mask-candidates")}}
	response := httptest.NewRecorder()
	server.Routes().ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/tenants/"+uuid.NewString()+"/projects/"+uuid.NewString()+"/interchange-jobs/"+uuid.NewString()+"/person-mask-candidates/"+uuid.NewString(), nil))
	if response.Code != 401 {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestInterchangeMaskPreviewHTTPRejectsInvalidBytes(t *testing.T) {
	id := uuid.New()
	for _, bad := range []bool{false, true} {
		reader := &interchangeMaskCandidateStub{bad: bad}
		response := httptest.NewRecorder()
		serveInterchangeMaskPreview(response, httptest.NewRequest("GET", "/", nil), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, id, id, id, id, id, id, reader)
		expected := 200
		if bad {
			expected = 503
		}
		if response.Code != expected || reader.calls != 1 {
			t.Fatalf("status=%d calls=%d", response.Code, reader.calls)
		}
	}
}
