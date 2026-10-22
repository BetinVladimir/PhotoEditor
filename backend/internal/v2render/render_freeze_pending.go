package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2draft"
	"time"
)

// A pending response is private execution evidence, never an HTTP response.
// The first compiler wins under the existing idempotency row lock. No object IO
// or domain row lock occurs while pinning it; registration still fences the draft.
type renderFreezePending struct {
	SchemaVersion   int             `json:"schema_version"`
	DocumentID      uuid.UUID       `json:"document_id"`
	DraftDocumentID uuid.UUID       `json:"draft_document_id"`
	DraftRevision   string          `json:"draft_revision"`
	Recipe          json.RawMessage `json:"recipe"`
}

func decodeRenderFreezePending(raw []byte, id uuid.UUID, request RenderFreezeRequest) ([]byte, error) {
	fail := domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	canonicalBody, err := canonical.Bytes(raw)
	if err != nil || len(canonicalBody) > 1049600 {
		return nil, fail
	}
	var pending renderFreezePending
	decoder := json.NewDecoder(bytes.NewReader(canonicalBody))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&pending) != nil || pending.SchemaVersion != 1 || pending.DocumentID != id || pending.DraftDocumentID != request.DocumentID || pending.DraftRevision != request.ExpectedRevision {
		return nil, fail
	}
	encoded, err := canonical.Marshal(pending)
	if err != nil || !bytes.Equal(encoded, canonicalBody) {
		return nil, fail
	}
	body, err := canonical.Bytes(pending.Recipe)
	if err != nil || len(body) > 1048576 {
		return nil, fail
	}
	if _, err = v2draft.DecodeFrozenRenderRecipe(body); err != nil {
		return nil, fail
	}
	return body, nil
}
func (s SQLRenderFreezeService) pinFreezePending(ctx context.Context, who auth.Identity, asset uuid.UUID, scope idempotency.Scope, request RenderFreezeRequest, id uuid.UUID, body []byte) ([]byte, bool, error) {
	payload, err := canonical.Marshal(renderFreezePending{1, id, request.DocumentID, request.ExpectedRevision, body})
	if err != nil {
		return nil, false, err
	}
	if _, err = decodeRenderFreezePending(payload, id, request); err != nil {
		return nil, false, err
	}
	hash, err := canonical.Hash(request)
	if err != nil {
		return nil, false, err
	}
	var pinned []byte
	completed := false
	err = tenant.WithTenant(ctx, s.Drafts.DB, who, scope.Tenant, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		var pid uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT project_id FROM photo.assets WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, scope.Tenant, asset).Scan(&pid); err != nil {
			return err
		}
		if _, err := project.Authorize(ctx, tx, scope.Tenant, pid, who.UserID, role, domain.Editor, false); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) VALUES($1,'user',$2,$3,$4,$5,$6) ON CONFLICT(tenant_id,principal_type,principal_id,route,key) DO NOTHING`, scope.Tenant, scope.Principal, scope.Route, scope.Key, hash, time.Now().Add(24*time.Hour)); err != nil {
			return err
		}
		var storedHash string
		var status *int
		var stored []byte
		var live bool
		if err := tx.QueryRow(ctx, `SELECT request_hash,response_status,response_body,expires_at>clock_timestamp() FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route=$3 AND key=$4 FOR UPDATE`, scope.Tenant, scope.Principal, scope.Route, scope.Key).Scan(&storedHash, &status, &stored, &live); err != nil {
			return err
		}
		if err := idempotency.ValidateReceiptAuthority(ctx,tx); err != nil { return err }
		if storedHash != hash {
			return domain.NewError("IDEMPOTENCY_CONFLICT", 409)
		}
		if status != nil {
			completed = true
			return nil
		}
		if !live {
			return domain.NewError("SNAPSHOT_EXPIRED", 410)
		}
		if len(stored) > 0 {
			var err error
			pinned, err = decodeRenderFreezePending(stored, id, request)
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE photo.idempotency_records SET response_body=$5 WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route=$3 AND key=$4`, scope.Tenant, scope.Principal, scope.Route, scope.Key, payload); err != nil {
			return err
		}
		pinned = append([]byte(nil), body...)
		return nil
	})
	return pinned, completed, err
}
