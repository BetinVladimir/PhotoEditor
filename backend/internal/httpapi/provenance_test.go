package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

type exportSignerStub struct {
	calls  int
	mode   string
	policy uuid.UUID
}

func (s *exportSignerStub) SignExport(_ context.Context, who auth.Identity, tid, export, policy uuid.UUID, key string) (v2input.JobTicket, error) {
	s.calls++
	s.policy = policy
	if s.mode == "conflict" {
		return v2input.JobTicket{}, artifactjson.ErrConflict
	}
	workflow := uuid.NewSHA1(tid, []byte("provenance:"+who.UserID.String()+":"+key))
	ticket := v2input.JobTicket{WorkflowID: workflow, RunID: uuid.NewSHA1(workflow, []byte("run")), State: "queued"}
	if s.mode == "wrong-ticket" {
		ticket.RunID = uuid.New()
	}
	return ticket, nil
}

func TestSignExportHTTPContract(t *testing.T) {
	for _, mode := range []string{"valid", "missing-key", "missing-policy", "null-policy", "zero-policy", "unknown", "duplicate", "trailing", "too-large", "media", "unconfigured", "wrong-ticket", "conflict"} {
		t.Run(mode, func(t *testing.T) {
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			tid, export, policy := uuid.New(), uuid.New(), uuid.New()
			body := `{"signing_policy_id":"` + policy.String() + `"}`
			expected := 202
			wantCalls := 1
			switch mode {
			case "missing-key", "missing-policy", "null-policy", "zero-policy", "unknown":
				expected = 422
				wantCalls = 0
			case "duplicate", "trailing":
				expected = 400
				wantCalls = 0
			case "too-large":
				expected = 413
				wantCalls = 0
			case "media":
				expected = 415
				wantCalls = 0
			case "unconfigured":
				expected = 503
				wantCalls = 0
			case "wrong-ticket":
				expected = 503
			case "conflict":
				expected = 409
			}
			switch mode {
			case "missing-policy":
				body = `{}`
			case "null-policy":
				body = `{"signing_policy_id":null}`
			case "zero-policy":
				body = `{"signing_policy_id":"00000000-0000-0000-0000-000000000000"}`
			case "unknown":
				body = `{"signing_policy_id":"` + policy.String() + `","key_handle":"forbidden"}`
			case "duplicate":
				body = `{"signing_policy_id":"` + policy.String() + `","signing_policy_id":"` + policy.String() + `"}`
			case "trailing":
				body += ` {}`
			case "too-large":
				body = strings.Repeat(" ", 4097) + body
			}
			request := httptest.NewRequest("POST", "/", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "sign")
			if mode == "missing-key" {
				request.Header.Del("Idempotency-Key")
			}
			if mode == "media" {
				request.Header.Set("Content-Type", "text/plain")
			}
			stub := &exportSignerStub{mode: mode}
			var signer ExportSigner = stub
			if mode == "unconfigured" {
				signer = nil
			}
			response := httptest.NewRecorder()
			serveSignExport(response, request, who, tid, export, signer)
			if response.Code != expected || stub.calls != wantCalls {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, stub.calls, response.Body.String())
			}
			if stub.calls > 0 && stub.policy != policy {
				t.Fatal("policy substituted")
			}
		})
	}
}

func TestSignExportRouteRequiresSession(t *testing.T) {
	server := &Server{Auth: &auth.Service{Pepper: []byte("sign-export")}}
	response := httptest.NewRecorder()
	server.Routes().ServeHTTP(response, httptest.NewRequest("POST", "/api/v1/tenants/"+uuid.NewString()+"/exports/"+uuid.NewString()+"/provenance", strings.NewReader(`{}`)))
	if response.Code != 401 {
		t.Fatalf("status=%d", response.Code)
	}
}

type signingPolicyListStub struct {
	items  []provenance.SigningPolicyChoice
	calls  int
	cancel context.CancelFunc
}

type deliverySigningPolicyListStub struct {
	items []provenance.DeliverySigningPolicyChoice
	seen  []string
	calls int
}

func (s *deliverySigningPolicyListStub) AvailableDeliverySigningPolicies(_ context.Context, _ auth.Identity, _, _ uuid.UUID, mimes []string) ([]provenance.DeliverySigningPolicyChoice, error) {
	s.calls++
	s.seen = append([]string(nil), mimes...)
	return s.items, nil
}

func TestDeliverySigningPolicyHTTPResponseIsBoundedAndSHAQualified(t *testing.T) {
	choice := provenance.DeliverySigningPolicyChoice{ID: uuid.New(), SHA256: strings.Repeat("a", 64), Revision: "1"}
	for _, mode := range []string{"valid", "empty", "bad_sha", "duplicate", "zero_id", "bad_revision", "unsorted"} {
		t.Run(mode, func(t *testing.T) {
			stub := &deliverySigningPolicyListStub{items: []provenance.DeliverySigningPolicyChoice{choice}}
			switch mode {
			case "empty":
				stub.items = nil
			case "bad_sha":
				stub.items[0].SHA256 = "A" + strings.Repeat("a", 63)
			case "duplicate":
				stub.items = append(stub.items, choice)
			case "zero_id":
				stub.items[0].ID = uuid.Nil
			case "bad_revision":
				stub.items[0].Revision = "01"
			case "unsorted":
				stub.items = []provenance.DeliverySigningPolicyChoice{{ID: uuid.MustParse("ffffffff-ffff-4fff-8fff-ffffffffffff"), SHA256: choice.SHA256, Revision: "1"}, choice}
			}
			response := httptest.NewRecorder()
			request := httptest.NewRequest("GET", "/", nil)
			serveDeliverySigningPolicies(response, request, auth.Identity{}, uuid.New(), uuid.New(), []string{"image/jpeg", "image/png"}, stub)
			want := 503
			if mode == "valid" || mode == "empty" {
				want = 200
			}
			if response.Code != want || stub.calls != 1 || len(stub.seen) != 2 {
				t.Fatal(mode, response.Code, stub.calls, stub.seen)
			}
			if mode == "valid" && (!strings.Contains(response.Body.String(), choice.SHA256) || strings.Contains(response.Body.String(), "operator-key")) {
				t.Fatal("redacted choice lost", response.Body.String())
			}
		})
	}
}

func TestDeliverySigningPolicyRouteRequiresSession(t *testing.T) {
	server := &Server{Auth: &auth.Service{Pepper: []byte("delivery-signing-policies")}}
	response := httptest.NewRecorder()
	server.Routes().ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/tenants/"+uuid.NewString()+"/projects/"+uuid.NewString()+"/delivery-signing-policies?mime=image%2Fjpeg", nil))
	if response.Code != 401 {
		t.Fatalf("status=%d", response.Code)
	}
}

func (s *signingPolicyListStub) AvailableSigningPolicies(context.Context, auth.Identity, uuid.UUID, uuid.UUID) ([]provenance.SigningPolicyChoice, error) {
	s.calls++
	if s.cancel != nil {
		s.cancel()
	}
	return s.items, nil
}

func TestSigningPolicyHTTPResponseMatchesBoundedClientContract(t *testing.T) {
	for _, mode := range []string{"valid", "empty", "maximum_revision", "maximum_count", "overflow_count", "duplicate", "zero_id", "zero_revision", "negative_revision", "leading_zero", "overflow_revision", "cancel_before", "cancel_after"} {
		t.Run(mode, func(t *testing.T) {
			items := []provenance.SigningPolicyChoice{{ID: uuid.New(), Revision: "1"}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stub := &signingPolicyListStub{}
			switch mode {
			case "empty":
				items = nil
			case "maximum_revision":
				items[0].Revision = "9223372036854775807"
			case "maximum_count", "overflow_count":
				count := 256
				if mode == "overflow_count" {
					count++
				}
				items = nil
				for i := 0; i < count; i++ {
					items = append(items, provenance.SigningPolicyChoice{ID: uuid.New(), Revision: "1"})
				}
			case "duplicate":
				items = append(items, items[0])
			case "zero_id":
				items[0].ID = uuid.Nil
			case "zero_revision":
				items[0].Revision = "0"
			case "negative_revision":
				items[0].Revision = "-1"
			case "leading_zero":
				items[0].Revision = "01"
			case "overflow_revision":
				items[0].Revision = "9223372036854775808"
			case "cancel_before":
				cancel()
			case "cancel_after":
				stub.cancel = cancel
			}
			stub.items = items
			request := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
			response := httptest.NewRecorder()
			serveSigningPolicies(response, request, auth.Identity{}, uuid.New(), uuid.New(), stub)
			success := mode == "valid" || mode == "empty" || mode == "maximum_revision" || mode == "maximum_count"
			if success {
				if response.Code != 200 {
					t.Fatal("valid catalogue refused", response.Code)
				}
				var result struct {
					Items []provenance.SigningPolicyChoice `json:"items"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Items == nil || len(result.Items) != len(items) {
					t.Fatal("invalid catalogue wire", err)
				}
			} else if response.Code == 200 {
				t.Fatal("invalid catalogue returned success")
			}
			if mode == "cancel_before" && stub.calls != 0 {
				t.Fatal("cancelled request reached provider")
			}
		})
	}
}
