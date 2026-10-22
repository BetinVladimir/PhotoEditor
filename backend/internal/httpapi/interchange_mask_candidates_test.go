package httpapi

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/interchange"
)

type interchangeMaskCandidateStub struct {
	calls         int
	bad           bool
	signatureOnly bool
	truncated     bool
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
	if s.signatureOnly {
		return []byte("\x89PNG\r\n\x1a\n"), nil
	}
	var output bytes.Buffer
	if err := png.Encode(&output, image.NewGray(image.Rect(0, 0, 2, 2))); err != nil {
		return nil, err
	}
	if s.truncated {
		return output.Bytes()[:33], nil
	}
	return output.Bytes(), nil
}

func (s *interchangeMaskCandidateStub) ReadEntry(_ context.Context, _ auth.Identity, _, _, _, entry uuid.UUID) ([]interchange.ImportPersonMaskCandidate, error) {
	s.calls++
	if s.bad {
		entry = uuid.New()
	}
	return []interchange.ImportPersonMaskCandidate{{ExternalID: "frame", Decision: interchange.ImportPersonMaskDecisionRequest{
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

func TestInterchangeMaskPreviewRejectsSignatureOnlyAndPreventsCaching(t *testing.T) {
	id := uuid.New()
	for _, signatureOnly := range []bool{false, true} {
		response := httptest.NewRecorder()
		serveInterchangeMaskPreview(response, httptest.NewRequest("GET", "/", nil), auth.Identity{}, id, id, id, id, id, id, &interchangeMaskCandidateStub{signatureOnly: signatureOnly})
		if signatureOnly {
			if response.Code != 503 {
				t.Fatalf("signature only status=%d", response.Code)
			}
			continue
		}
		if response.Code != 200 || response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("status=%d headers=%v", response.Code, response.Header())
		}
	}
}

func TestImportMaskEntriesValidateResponseScope(t *testing.T) {
	valid := interchange.ImportPersonMaskCandidateEntry{EntryID: uuid.New(), TargetVersionID: uuid.New(), AssetID: uuid.New(), ExternalID: "frame"}
	if err := validateImportMaskEntries([]interchange.ImportPersonMaskCandidateEntry{valid}); err != nil {
		t.Fatal(err)
	}
	if err := validateImportMaskEntries([]interchange.ImportPersonMaskCandidateEntry{valid, valid}); err == nil {
		t.Fatal("accepted duplicate entry")
	}
	for _, change := range []func(*interchange.ImportPersonMaskCandidateEntry){func(v *interchange.ImportPersonMaskCandidateEntry) { v.AssetID = uuid.Nil }, func(v *interchange.ImportPersonMaskCandidateEntry) { v.TargetVersionID = uuid.Nil }, func(v *interchange.ImportPersonMaskCandidateEntry) { v.ExternalID = "" }, func(v *interchange.ImportPersonMaskCandidateEntry) { v.ExternalID = strings.Repeat("x", 241) }} {
		item := valid
		change(&item)
		if err := validateImportMaskEntries([]interchange.ImportPersonMaskCandidateEntry{item}); err == nil {
			t.Fatal("accepted invalid entry")
		}
	}
	if err := validateImportMaskEntries(make([]interchange.ImportPersonMaskCandidateEntry, 5001)); err == nil {
		t.Fatal("accepted oversized page")
	}
}

func TestImportMaskCandidatesRejectDuplicateAndOversizedEvidence(t *testing.T) {
	entry := uuid.New()
	valid := interchange.ImportPersonMaskCandidate{ExternalID: "frame", Decision: interchange.ImportPersonMaskDecisionRequest{Action: "grant", EntryID: entry, TargetVersionID: uuid.New(), OverrideArtifactID: uuid.New(), MaskArtifactID: uuid.New()}}
	if err := validateImportMaskCandidates(entry, []interchange.ImportPersonMaskCandidate{valid}); err != nil {
		t.Fatal(err)
	}
	if err := validateImportMaskCandidates(entry, []interchange.ImportPersonMaskCandidate{valid, valid}); err == nil {
		t.Fatal("accepted duplicate candidate")
	}
	bad := valid
	bad.ExternalID = ""
	if err := validateImportMaskCandidates(entry, []interchange.ImportPersonMaskCandidate{bad}); err == nil {
		t.Fatal("accepted empty external ID")
	}
	if err := validateImportMaskCandidates(entry, make([]interchange.ImportPersonMaskCandidate, 5001)); err == nil {
		t.Fatal("accepted oversized candidates")
	}
	if err := validateImportMaskCandidates(uuid.New(), []interchange.ImportPersonMaskCandidate{valid}); err == nil {
		t.Fatal("accepted foreign entry")
	}
}

func TestImportMaskPreviewRejectsTruncatedPNGAfterValidHeader(t *testing.T) {
	id := uuid.New()
	response := httptest.NewRecorder()
	serveInterchangeMaskPreview(response, httptest.NewRequest("GET", "/", nil), auth.Identity{}, id, id, id, id, id, id, &interchangeMaskCandidateStub{truncated: true})
	if response.Code != 503 {
		t.Fatalf("truncated PNG status=%d", response.Code)
	}
}
