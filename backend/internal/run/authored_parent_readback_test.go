package run

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"strings"
	"testing"
)

func TestAuthoredParentReadbackBindsActualReceipt(t *testing.T) {
	tenantID, projectID, parent := uuid.New(), uuid.New(), uuid.New()
	pin := strings.Repeat("a", 64)
	raw, _ := json.Marshal(map[string]any{"parent_run_id": parent, "request_sha": pin})
	out, err := decodeAuthoredParentLink(raw, tenantID, projectID, parent, pin)
	if err != nil || out.Parent != parent || out.Tenant != tenantID || out.Project != projectID || out.RequestSHA != pin || out.Admission != "accepted" {
		t.Fatal(out, err)
	}
	for name, body := range map[string][]byte{"foreignparent": []byte(`{"parent_run_id":"` + uuid.NewString() + `","request_sha":"` + pin + `"}`), "extra": []byte(`{"parent_run_id":"` + parent.String() + `","request_sha":"` + pin + `","source":"fake"}`), "missing": []byte(`{}`), "null": []byte(`null`), "tail": append(append([]byte{}, raw...), []byte(`{}`)...)} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeAuthoredParentLink(body, tenantID, projectID, parent, pin); err == nil {
				t.Fatal("unbound receipt accepted")
			}
		})
	}
	_, err = decodeAuthoredParentLink(raw, tenantID, projectID, parent, strings.Repeat("b", 64))
	var de *domain.Error
	if !errors.As(err, &de) || de.Code != "IDEMPOTENCY_CONFLICT" {
		t.Fatal("request collision lost", err)
	}
}
func TestAuthoredParentReadbackCancelledAndNoDependency(t *testing.T) {
	s := EditorEffectsService{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.ReadAuthoredParent(ctx, auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), strings.Repeat("a", 64)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	_, err := s.ReadAuthoredParent(context.Background(), who, uuid.New(), uuid.New(), uuid.New(), strings.Repeat("a", 64))
	var de *domain.Error
	if !errors.As(err, &de) || de.Code != "DEPENDENCY_UNAVAILABLE" {
		t.Fatal(err)
	}
}

func TestAuthoredParentRequestCanonicalClientGolden(t *testing.T) {
	for _, vector := range []struct{ body, sha string }{{`{"draft_document_id":"22222222-2222-4222-8222-222222222222","draft_revision":1,"frozen_document_id":"44444444-4444-4444-8444-444444444444","recipe":{"artifact_id":"33333333-3333-4333-8333-333333333333","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"source":{"asset_version_id":"11111111-1111-4111-8111-111111111111","canvas_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","source":{"artifact_id":"22222222-2222-4222-8222-222222222222","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}`, "3652ba97071f4a9d66726a5c53f7ffcfd250e761fd5a957fae779b958ff933ba"}, {`{"draft_document_id":"22222222-2222-4222-8222-222222222222","draft_revision":9007199254740991,"frozen_document_id":"44444444-4444-4444-8444-444444444444","recipe":{"artifact_id":"33333333-3333-4333-8333-333333333333","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"source":{"asset_version_id":"11111111-1111-4111-8111-111111111111","canvas_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","source":{"artifact_id":"22222222-2222-4222-8222-222222222222","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}`, "f163ebebdae8820f134437a034335be72b4dabed2d25d0153d03c0ef7bcb48e9"}} {
		var req EditorEffectRequest
		if err := json.Unmarshal([]byte(vector.body), &req); err != nil {
			t.Fatal(err)
		}
		actual, err := canonical.Hash(req)
		if err != nil || actual != vector.sha {
			t.Fatal("request canonical wire changed", actual, err)
		}
	}
}
