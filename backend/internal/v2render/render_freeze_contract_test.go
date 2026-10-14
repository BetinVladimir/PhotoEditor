package v2render

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestRenderFreezeRequestStrictShapeAndIdentity(t *testing.T) {
	request := RenderFreezeRequest{DocumentID: uuid.New(), ExpectedRevision: "12"}
	raw, _ := canonical.Marshal(request)
	decoded, err := DecodeRenderFreezeRequest(raw)
	if err != nil || decoded != request {
		t.Fatal("valid request", err)
	}
	for _, bad := range []string{
		`{}`, `null`, strings.Replace(string(raw), `"document_id"`, `"Document_ID"`, 1), strings.Replace(string(raw), `"assume_profile":null,`, "", 1), strings.Replace(string(raw), `"document_id":`, `"model_lock_sha":"`+strings.Repeat("a", 64)+`","document_id":`, 1), strings.Replace(string(raw), `"document_id":`, `"assume_profile":null,"document_id":`, 1), string(raw) + `{}`,
	} {
		if _, err := DecodeRenderFreezeRequest([]byte(bad)); err == nil {
			t.Fatal("invalid request accepted", bad)
		}
	}
	for _, revision := range []string{"0", "01", "+1", "-1", "1.0", "", "9223372036854775808"} {
		invalid := request
		invalid.ExpectedRevision = revision
		body, _ := json.Marshal(invalid)
		if _, err := DecodeRenderFreezeRequest(body); err == nil {
			t.Fatal("invalid revision", revision)
		}
	}
	tenant, actor, asset := uuid.New(), uuid.New(), uuid.New()
	id, err := RenderFreezeDocumentID(tenant, actor, asset, "key", request)
	again, _ := RenderFreezeDocumentID(tenant, actor, asset, "key", request)
	other, _ := RenderFreezeDocumentID(tenant, actor, uuid.New(), "key", request)
	if err != nil || id == uuid.Nil || id != again || id == other {
		t.Fatal("scope/identity", err)
	}
	invalidProfile := "unknown"
	request.AssumeProfile = &invalidProfile
	if _, err := RenderFreezeDocumentID(tenant, actor, asset, "key", request); err == nil {
		t.Fatal("unsupported ICC assumption")
	}
}

func TestRenderFreezeResponseStrictBindings(t *testing.T) {
	request := RenderFreezeRequest{DocumentID: uuid.New(), ExpectedRevision: "1"}
	id := uuid.New()
	result := RenderFreezeResult{id, request.DocumentID, "1", v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, CanvasHash: strings.Repeat("b", 64)}, v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}}
	raw, _ := canonical.Marshal(result)
	if _, err := DecodeRenderFreezeResult(raw, id, request); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*RenderFreezeResult){func(r *RenderFreezeResult) { r.DocumentID = uuid.New() }, func(r *RenderFreezeResult) { r.DraftDocumentID = uuid.New() }, func(r *RenderFreezeResult) { r.DraftRevision = "2" }, func(r *RenderFreezeResult) { r.Recipe.SHA256 = "invalid" }, func(r *RenderFreezeResult) { r.Source.AssetVersionID = uuid.Nil }} {
		changed := result
		mutate(&changed)
		body, _ := canonical.Marshal(changed)
		if _, err := DecodeRenderFreezeResult(body, id, request); err == nil {
			t.Fatal("changed result accepted")
		}
	}
	extra := strings.Replace(string(raw), `"recipe":`, `"runtime":"cuda","recipe":`, 1)
	if _, err := DecodeRenderFreezeResult([]byte(extra), id, request); err == nil {
		t.Fatal("additional response field accepted")
	}
}
