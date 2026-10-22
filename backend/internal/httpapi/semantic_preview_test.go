package httpapi

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/semanticreview"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"photoeditor/backend/internal/auth"
	"strings"
	"testing"
)

func TestSemanticPreApplyPreviewRouteRequiresAuthentication(t *testing.T) {
	server := &Server{Auth: &auth.Service{Pepper: []byte("semantic-preapply-preview")}}
	handler := server.Routes()
	found := false
	if err := chi.Walk(handler.(chi.Routes), func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method == "GET" && path == "/api/v1/tenants/{t}/semantic-intents/{id}/targets/{target}/preview" {
			found = true
		}
		return nil
	}); err != nil || !found {
		t.Fatal("preview route missing", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/tenants/"+uuid.NewString()+"/semantic-intents/"+uuid.NewString()+"/targets/"+uuid.NewString()+"/preview", nil))
	if response.Code != 401 {
		t.Fatal("unauthenticated preview accepted", response.Code)
	}
}

func TestSemanticPreviewQueryRejectsMalformedOrAmbiguousBindings(t *testing.T) {
	valid := "expected_revision=1&target_revision=2&plan_sha=" + strings.Repeat("a", 64) + "&candidate_sha=" + strings.Repeat("b", 64)
	request, err := semanticPreviewQuery(valid)
	if err != nil || request.ExpectedRevision != "1" || request.TargetRevision != "2" {
		t.Fatal("valid preview fence rejected")
	}
	for _, query := range []string{valid + "&ignored=%ZZ", valid + "&expected_revision=3", valid + "&unexpected=1", strings.Replace(valid, "target_revision=2", "target_revision=02", 1), strings.Replace(valid, "candidate_sha=", "candidate_sha=%ZZ", 1), ""} {
		if _, err := semanticPreviewQuery(query); err == nil {
			t.Fatal("malformed preview query accepted", query)
		}
	}
}

func TestSemanticPreviewAdmissionResponsePreservesRecoveryIdentity(t *testing.T) {
	for _, mode := range []string{"new", "recovered", "recovered_cancelling", "uncertain", "missing_task", "unknown_status", "draft", "cancelled_request"} {
		t.Run(mode, func(t *testing.T) {
			result := semanticreview.PreviewAdmission{RunID: uuid.New(), TaskID: uuid.New(), RunStatus: "queued", CommitAttempted: true}
			var failure error
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "recovered": result.Recovered = true; result.RunStatus = "completed"
			case "recovered_cancelling": result.Recovered = true; result.RunStatus = "cancelling"
			case "uncertain": failure = semanticreview.ErrUnavailable
			case "missing_task": result.TaskID = uuid.Nil
			case "unknown_status": result.RunStatus = "unrecognized"
			case "draft": result.RunStatus = "draft"
			case "cancelled_request": cancel()
			}
			request := httptest.NewRequest("POST", "/preview", nil).WithContext(ctx)
			response := httptest.NewRecorder()
			writeSemanticPreviewAdmission(response, request, result, failure)
			if response.Header().Get("X-Preview-Run-ID") != result.RunID.String() { t.Fatal("recovery identity lost") }
			if mode == "cancelled_request" {
				if response.Body.Len() != 0 { t.Fatal("cancelled response written") }
				return
			}
			if mode != "new" && mode != "recovered" && mode != "recovered_cancelling" {
				if response.Code != 503 { t.Fatal("partial admission accepted", response.Code) }
				return
			}
			var body struct { Status string `json:"status"`; RunID uuid.UUID `json:"run_id"`; TaskID uuid.UUID `json:"task_id"`; Recovered bool `json:"recovered"`; CommitAttempted bool `json:"commit_attempted"` }
			if response.Code != 202 || json.Unmarshal(response.Body.Bytes(), &body) != nil || body.Status != result.RunStatus || body.RunID != result.RunID || body.TaskID != result.TaskID || body.Recovered != result.Recovered || body.CommitAttempted != result.CommitAttempted { t.Fatal("admission response changed", response.Body.String()) }
		})
	}
}

func TestSemanticPreviewQueryNeverAcceptsExecutionOverrides(t *testing.T) {
	valid := "expected_revision=1&target_revision=2&plan_sha=" + strings.Repeat("a", 64) + "&candidate_sha=" + strings.Repeat("b", 64)
	for _, field := range []string{"backend", "ram_bytes", "vram_bytes", "scratch_bytes", "output_bytes", "pixel_count", "execution_profile", "qualification", "model_lock_sha", "runtime_lock_sha", "device_profile_sha", "retouch_context", "beauty", "smoothing", "faces", "tenant_id"} {
		t.Run(field, func(t *testing.T) {
			if _, err := semanticPreviewQuery(valid + "&" + field + "=1"); err == nil { t.Fatal("client execution override accepted", field) }
		})
	}
}

func TestSemanticPreviewPostRequiresAuthentication(t *testing.T) {
	server := &Server{Auth: &auth.Service{Pepper: []byte("semantic-preview-post")}}
	handler := server.Routes()
	found := false
	if err := chi.Walk(handler.(chi.Routes), func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method == "POST" && path == "/api/v1/tenants/{t}/semantic-intents/{id}/targets/{target}/preview" { found = true }
		return nil
	}); err != nil || !found { t.Fatal("preview admission route missing", err) }
	for _, body := range []string{"", `{}`, `{"vram_bytes":1}`} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest("POST", "/api/v1/tenants/"+uuid.NewString()+"/semantic-intents/"+uuid.NewString()+"/targets/"+uuid.NewString()+"/preview", strings.NewReader(body))
		handler.ServeHTTP(response, request)
		if response.Code != 401 || response.Header().Get("X-Preview-Run-ID") != "" { t.Fatal("unauthenticated admission escaped", response.Code) }
	}
}

func TestSemanticPreviewRecoveredStatusesRemainAvailable(t *testing.T) {
	for _, status := range []string{"queued", "running", "completed", "failed", "cancelled", "cancelling"} {
		t.Run(status, func(t *testing.T) {
			result := semanticreview.PreviewAdmission{RunID: uuid.New(), TaskID: uuid.New(), RunStatus: status, Recovered: true}
			response := httptest.NewRecorder()
			writeSemanticPreviewAdmission(response, httptest.NewRequest("POST", "/preview", nil), result, nil)
			if response.Code != 202 { t.Fatal("saved status hidden", status, response.Code) }
		})
	}
}
