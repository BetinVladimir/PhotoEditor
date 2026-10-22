package httpapi

import (
	"context"
	"github.com/google/uuid"
	"net/http/httptest"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/interchange"
	"strings"
	"testing"
)

type archiveBeginHTTPStub struct {
	calls              int
	corrupt, reconcile bool
}

func (s *archiveBeginHTTPStub) Begin(ctx context.Context, who auth.Identity, tid uuid.UUID, key string, request interchange.ArchiveUploadRequest) (interchange.ArchiveUploadIntent, interchange.ArchiveUploadReservation, error) {
	if s.reconcile {
		s.calls++
		return interchange.ArchiveUploadIntent{}, interchange.ArchiveUploadReservation{}, domain.NewError("ARCHIVE_BEGIN_RECONCILIATION_REQUIRED", 409)
	}
	s.calls++
	if s.corrupt {
		tid = uuid.New()
	}
	p, err := interchange.FreezeArchiveUpload(ctx, tid, who, key, request)
	return p, interchange.ArchiveUploadReservation{Token: uuid.New(), State: "uploading", MultipartID: "private-multipart"}, err
}
func TestInterchangeArchiveBeginHTTP(t *testing.T) {
	for _, mode := range []string{"ok", "corrupt", "reconciliation", "invalid", "missing-key", "uninstalled", "unknown-field"} {
		t.Run(mode, func(t *testing.T) {
			body := `{"filename":"archive.zip","bytes":22,"sha256":"` + strings.Repeat("a", 64) + `"}`
			key := "key"
			status := 201
			stub := &archiveBeginHTTPStub{corrupt: mode == "corrupt", reconcile: mode == "reconciliation"}
			var service InterchangeArchiveBeginner = stub
			switch mode {
			case "corrupt":
				status = 503
			case "reconciliation":
				status = 409
			case "invalid":
				body = `{"filename":"a.jpg","bytes":22}`
				status = 422
			case "missing-key":
				key = ""
				status = 422
			case "uninstalled":
				service = nil
				status = 503
			case "unknown-field":
				body = `{"project_id":"foreign"}`
				status = 422
			}
			r := httptest.NewRequest("POST", "/", strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Idempotency-Key", key)
			w := httptest.NewRecorder()
			serveInterchangeArchiveBegin(w, r, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), service)
			if w.Code != status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if mode == "reconciliation" && !strings.Contains(w.Body.String(), "ARCHIVE_BEGIN_RECONCILIATION_REQUIRED") {
				t.Fatal("reconciliation code lost at HTTP boundary")
			}
			if status == 400 || status == 422 {
				if stub.calls != 0 {
					t.Fatal("invalid request reached service")
				}
			}
			if strings.Contains(w.Body.String(), "private-multipart") || strings.Contains(w.Body.String(), "object_key") || strings.Contains(w.Body.String(), "token") {
				t.Fatal("internal upload authority leaked")
			}
		})
	}
}
