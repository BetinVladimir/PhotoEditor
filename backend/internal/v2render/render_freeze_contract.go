package v2render

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
	"strconv"
)

const MaxRenderFreezeBytes = 4096

type RenderFreezeRequest struct {
	DocumentID       uuid.UUID `json:"document_id"`
	ExpectedRevision string    `json:"expected_revision"`
	AssumeProfile    *string   `json:"assume_profile"`
}

type RenderFreezeResult struct {
	DocumentID      uuid.UUID                     `json:"document_id"`
	DraftDocumentID uuid.UUID                     `json:"draft_document_id"`
	DraftRevision   string                        `json:"draft_revision"`
	Source          v2input.SessionVersionBinding `json:"source"`
	Recipe          v2input.Ref                   `json:"recipe"`
}

func renderFreezeRevision(request RenderFreezeRequest) (int64, error) {
	text := request.ExpectedRevision
	if request.DocumentID == uuid.Nil || len(text) < 1 || len(text) > 19 || text[0] == '0' {
		return 0, v2input.ErrInvalid
	}
	for _, c := range text {
		if c < '0' || c > '9' {
			return 0, v2input.ErrInvalid
		}
	}
	revision, err := strconv.ParseInt(text, 10, 64)
	if err != nil || revision < 1 {
		return 0, v2input.ErrInvalid
	}
	if request.AssumeProfile != nil && *request.AssumeProfile != "srgb_v1" && *request.AssumeProfile != "linear_srgb_v1" && *request.AssumeProfile != "work_rec2020_gamma24_v1" {
		return 0, v2input.ErrInvalid
	}
	return revision, nil
}

func DecodeRenderFreezeRequest(raw []byte) (RenderFreezeRequest, error) {
	var out RenderFreezeRequest
	if len(raw) < 2 || len(raw) > MaxRenderFreezeBytes {
		return out, v2input.ErrInvalid
	}
	body, err := canonical.Bytes(raw)
	if err != nil {
		return out, v2input.ErrInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || len(fields) != 3 || len(fields["document_id"]) == 0 || string(fields["document_id"]) == "null" || len(fields["expected_revision"]) == 0 || string(fields["expected_revision"]) == "null" || len(fields["assume_profile"]) == 0 || json.Unmarshal(body, &out) != nil {
		return out, v2input.ErrInvalid
	}
	if _, err := renderFreezeRevision(out); err != nil {
		return RenderFreezeRequest{}, err
	}
	return out, nil
}

func RenderFreezeDocumentID(tenant, actor, asset uuid.UUID, key string, request RenderFreezeRequest) (uuid.UUID, error) {
	if tenant == uuid.Nil || actor == uuid.Nil || asset == uuid.Nil || !idempotency.ValidKey(key) {
		return uuid.Nil, v2input.ErrInvalid
	}
	if _, err := renderFreezeRevision(request); err != nil {
		return uuid.Nil, err
	}
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("v26-render-freeze/"+tenant.String()+"/"+actor.String()+"/"+asset.String()+"/"+key)), nil
}

func ValidateRenderFreezeResult(id uuid.UUID, request RenderFreezeRequest, result RenderFreezeResult) error {
	if id == uuid.Nil || result.DocumentID != id || result.DraftDocumentID != request.DocumentID || result.DraftRevision != request.ExpectedRevision || result.Source.AssetVersionID == uuid.Nil || result.Source.Source.ArtifactID == uuid.Nil || result.Recipe.ArtifactID == uuid.Nil {
		return v2input.ErrInvalid
	}
	for _, digest := range []string{result.Source.CanvasHash, result.Source.Source.SHA256, result.Recipe.SHA256} {
		if _, err := domain.ParseDigest(digest); err != nil {
			return v2input.ErrInvalid
		}
	}
	return nil
}

func DecodeRenderFreezeResult(raw []byte, id uuid.UUID, request RenderFreezeRequest) (RenderFreezeResult, error) {
	var out RenderFreezeResult
	body, err := canonical.Bytes(raw)
	if err != nil || len(body) > MaxRenderFreezeBytes {
		return out, v2input.ErrInvalid
	}
	fields, err := renderSubmissionFields(body, "document_id", "draft_document_id", "draft_revision", "source", "recipe")
	if err != nil {
		return out, err
	}
	source, err := renderSubmissionFields(fields["source"], "asset_version_id", "source", "canvas_hash")
	if err != nil {
		return out, err
	}
	if _, err := renderSubmissionFields(source["source"], "artifact_id", "sha256"); err != nil {
		return out, err
	}
	if _, err := renderSubmissionFields(fields["recipe"], "artifact_id", "sha256"); err != nil {
		return out, err
	}
	if json.Unmarshal(body, &out) != nil || ValidateRenderFreezeResult(id, request, out) != nil {
		return RenderFreezeResult{}, v2input.ErrInvalid
	}
	return out, nil
}
