package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/config"
	"photoeditor/backend/internal/httpapi"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/semanticreview"
)

// The two invocations straddle a real leased preview worker and adoption.
// No artifact, task result, presentation or selected proof is synthesized here.
func shownCapturePhase(t *testing.T, ctx context.Context, api, ownerDB *pgxpool.Pool, store *objectstore.Store, bucket string,
	authentication *auth.Service, token string, who auth.Identity, tenant, intent uuid.UUID,
	request *semanticreview.ApplyDecision, capturedPreview *json.RawMessage, catalog semanticreview.ExposurePreparationCatalog, provider *semanticreview.ExposurePreviewProvider) bool {
	t.Helper()
	phase := os.Getenv("PHOTO_TEST_V27_SHOWN_PHASE")
	if phase == "" {
		return false
	}
	if len(request.TargetDecisions) != 1 || request.TargetDecisions[0].Decision != "approve" {
		t.Fatal("shown fixture requires one explicit approval")
	}
	target := request.TargetDecisions[0].TargetID
	// Candidate identity comes from the current server projection, never a fixture guess.
	reader, err := semanticreview.NewPlanObjectReader(store, bucket)
	if err != nil {
		t.Fatal(err)
	}
	kind, err := semanticreview.SQLReviewKindReader(api, reader)
	if err != nil {
		t.Fatal(err)
	}
	entry := catalog.Tenants[0]
	admission := &tenantPreviewAdmission{services: map[uuid.UUID]*semanticreview.ExposurePreviewAdmissionService{tenant: {DB: api, Objects: store, Reader: reader, Bucket: bucket, ReviewBudget: entry.Review.DocumentBudgetBytes, ReviewModelSHA: entry.Review.ModelSHA, ReviewDatasetSHA: entry.Review.DatasetSHA, OwnerCosts: entry.Execution.OwnerCosts, Limits: entry.Execution.Limits, Qualification: entry.Execution.Qualification}}}
	admission.resolve, err = semanticreview.SQLPreviewKindResolver(api, reader)
	if err != nil {
		t.Fatal(err)
	}
	trace := &shownAdmissionTrace{t: t, next: admission, owner: ownerDB}
	handler := (&httpapi.Server{DB: api, Auth: authentication, SemanticReviewKind: kind, SemanticPreviewAdmission: trace, SemanticReviewPreview: provider, Config: config.Config{Environment: "development", AppOrigin: "http://localhost:8080", PortalOrigin: "http://localhost:3000", MaxJSONBytes: 65536}}).Routes()
	call := func(method, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil).WithContext(ctx)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-Photo-Client", "native")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	base := "/api/v1/tenants/" + tenant.String() + "/semantic-intents/" + intent.String()
	page := call("GET", base+"/targets")
	if page.Code != 200 {
		t.Fatal("shown target projection", page.Code, page.Body.String())
	}
	var targets struct {
		Items []semanticreview.TargetResource `json:"items"`
	}
	if json.Unmarshal(page.Body.Bytes(), &targets) != nil {
		t.Fatal("shown targets decode")
	}
	var query url.Values
	for _, row := range targets.Items {
		if row.ID == target && row.Document != nil {
			query = url.Values{"expected_revision": {request.ExpectedRevision}, "target_revision": {strconv.FormatInt(row.Revision, 10)}, "plan_sha": {request.PlanSHA}, "candidate_sha": {row.Document.SHA256}}
		}
	}
	if query == nil {
		t.Fatal("shown target missing")
	}
	path := base + "/targets/" + target.String() + "/preview?" + query.Encode()
	if phase == "admit" {
		w := call(http.MethodPost, path)
		// The generic tenant fixture has only 10,000,000 storage bytes. A real
		// 256px preview requires at least 10,764,288 output bytes, even before
		// existing usage. Prove refusal, then simulate an explicit operator
		// allowance increase and recover the SAME immutable saved draft.
		if w.Code != http.StatusConflict || trace.last.RunID == uuid.Nil || trace.last.CommitAttempted {
			t.Fatal("low-capacity preview must refuse before commit", w.Code, w.Body.String())
		}
		originalRun := trace.last.RunID
		var emptyDraft bool
		if e := ownerDB.QueryRow(ctx, `SELECT r.status='draft' AND NOT r.graph_sealed AND r.v27_preapply_preview_profile IS NULL
		 AND NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id)
		 AND NOT EXISTS(SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=r.tenant_id AND q.holder_type='run' AND q.holder_id=r.id)
		 FROM photo.runs r WHERE r.tenant_id=$1 AND r.id=$2`, tenant, originalRun).Scan(&emptyDraft); e != nil || !emptyDraft {
			t.Fatal("low-capacity refusal left work or reservation", e)
		}
		changed, e := ownerDB.Exec(ctx, `UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','1073741824'::jsonb)
		 WHERE id=$1 AND (quotas->>'storage_bytes')::bigint=10000000`, tenant)
		if e != nil || changed.RowsAffected() != 1 {
			t.Fatal("isolated fixture operator allowance", e)
		}
		w = call(http.MethodPost, path)
		if trace.last.RunID != originalRun {
			t.Fatal("capacity recovery changed immutable preview identity")
		}
		if w.Code != 202 {
			t.Fatal("actual preview admission", w.Code, w.Body.String())
		}
		if err = os.WriteFile(os.Getenv("PHOTO_TEST_V27_SHOWN_ADMISSION_FILE"), w.Body.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		t.Log("V27_SHOWN_PREVIEW_ADMITTED")
		return true
	}
	if phase != "apply" {
		t.Fatal("unknown shown fixture phase")
	}
	w := call(http.MethodGet, path)
	if w.Code != 200 {
		t.Fatal("actual preview selection", w.Code, w.Body.String())
	}
	var preview semanticreview.Preview
	if json.Unmarshal(w.Body.Bytes(), &preview) != nil || preview.Validate(tenant, intent, target, semanticreview.PreviewRequest{ExpectedRevision: request.ExpectedRevision, TargetRevision: query.Get("target_revision"), PlanSHA: request.PlanSHA, CandidateSHA: query.Get("candidate_sha")}, time.Now()) != nil {
		t.Fatal("actual preview binding")
	}
	*capturedPreview = append(json.RawMessage(nil), w.Body.Bytes()...)
	request.TargetDecisions[0].Preview = &semanticreview.PreviewSelection{Render: preview.Render, Evidence: preview.Evidence}
	return false
}

// Trace the single authoritative request without probing any mutating stage twice.
type shownAdmissionTrace struct {
	t     *testing.T
	next  httpapi.SemanticPreviewAdmission
	owner *pgxpool.Pool
	last  semanticreview.PreviewAdmission
}

func (s *shownAdmissionTrace) AdmitPreview(ctx context.Context, who auth.Identity, tenant, intent, target uuid.UUID, request semanticreview.PreviewRequest) (semanticreview.PreviewAdmission, error) {
	result, err := s.next.AdmitPreview(ctx, who, tenant, intent, target, request)
	s.last = result
	s.t.Log("shown actual admission stage", "run", result.RunID, "task", result.TaskID, "commit attempted", result.CommitAttempted, "state", result.RunStatus, "recovered", result.Recovered, "error", err)

	return result, err
}
