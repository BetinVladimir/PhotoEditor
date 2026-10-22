package contactsheet

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
)

// replay recovers an accepted receipt without preparing resources again.
// Current membership and project access are required even for expired selections.
// A miss is only a preflight: Create still uses the transactional idempotency gate.
func (s Service) replay(ctx context.Context, who auth.Identity, tid uuid.UUID, key string, request CreateRequest) (idempotency.Response, bool, error) {
	var out idempotency.Response
	var found bool
	if !idempotency.ValidKey(key) {
		return out, false, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400)
	}
	hash, err := canonical.Hash(request)
	if err != nil { return out, false, err }
	err = tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		var projectID uuid.UUID
		err := tx.QueryRow(ctx, `SELECT c.project_id FROM photo.snapshots s
		 JOIN photo.collections c ON c.tenant_id=s.tenant_id AND c.id=s.collection_id
		 AND c.deleted_at IS NULL WHERE s.tenant_id=$1 AND s.id=$2
		 AND s.kind='selection' AND s.principal_type='user' AND s.principal_id=$3`,
			tid, request.SelectionSnapshotID, who.UserID).Scan(&projectID)
		if errors.Is(err, pgx.ErrNoRows) { return domain.NewError("NOT_FOUND", 404) }
		if err != nil { return err }
		if _, err = project.Authorize(ctx, tx, tid, projectID, who.UserID, role, domain.Editor, false); err != nil { return err }
		var storedHash string
		var status *int
		var body []byte
		err = tx.QueryRow(ctx, `SELECT request_hash,response_status,response_body,resource_id
		 FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_type='user'
		 AND principal_id=$2 AND route='ContactSheet.Create' AND key=$3 FOR UPDATE`,
			tid, who.UserID, key).Scan(&storedHash, &status, &body, &out.ResourceID)
		if errors.Is(err, pgx.ErrNoRows) { return nil }
		if err != nil { return err }
		if storedHash != hash { return domain.NewError("IDEMPOTENCY_CONFLICT", 409) }
		if status == nil { return nil }
		out.Body, err = canonical.Bytes(body)
		if err != nil { return err }
		out.Status, out.Replayed, found = *status, true, true
		if err=authorizeSelection(ctx,tx,who,tid,request);err!=nil{return err}
		if err=validateReceipt(ctx,tx,who,tid,key,request,out);err!=nil{return err}
		if isAbandoned(out,request,key){return domain.NewError("CONTACT_SHEET_ABANDONED",409)}
		if err:=authorizeSelection(ctx,tx,who,tid,request);err!=nil{return err}
		return ctx.Err()
	})
	return out, found, err
}
