package delivery

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
)

// AdmitPreparedCreate commits the proof and package together after independently
// registered task inputs. It reads the authoritative pending record under lock;
// callers cannot substitute a different catalog or recipe inventory. It does not
// activate the workflow or return a completed public CreateDelivery response.
func (s Service) AdmitPreparedCreate(ctx context.Context, who auth.Identity, tid uuid.UUID, request CreateRequest, key string) error {
 return s.admitPreparedCreate(ctx,who,tid,request,key,nil,false)
}
func(s Service)AdmitPreparedWatermarkCreate(ctx context.Context,who auth.Identity,tid uuid.UUID,request CreateRequest,key string,pins []PreparedWatermarkPin)error{return s.admitPreparedCreate(ctx,who,tid,request,key,pins,true)}
func(s Service)admitPreparedCreate(ctx context.Context,who auth.Identity,tid uuid.UUID,request CreateRequest,key string,pins []PreparedWatermarkPin,watermark bool)error{
	if err := ctx.Err(); err != nil {
		return err
	}
	intent, err := FreezeCreateIntent(tid, who, request, key)
	if err != nil {
		return err
	}
	if s.DB == nil {
		return domain.NewError("DELIVERY_UNAVAILABLE", 503)
	}
	return tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var authorized bool
		if err := tx.QueryRow(ctx, `SELECT photo.v2_editor_authorized($1,$2,$3,$4)`, tid, request.ProjectID, who.UserID, who.SessionID).Scan(&authorized); err != nil {
			return err
		}
		if !authorized {
			return domain.NewError("NOT_FOUND", 404)
		}
		var hash string
		var status *int
		var saved []byte
		var live bool
		err := tx.QueryRow(ctx, `SELECT request_hash,response_status,response_body,expires_at>clock_timestamp() FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route='POST /delivery-packages' AND key=$3 FOR UPDATE`, tid, who.UserID, key).Scan(&hash, &status, &saved, &live)
		if err == pgx.ErrNoRows {
			return domain.NewError("DELIVERY_CREATE_STALE", 409)
		}
		if err != nil {
			return err
		}
		if hash != intent.requestSHA {
			return domain.NewError("IDEMPOTENCY_CONFLICT", 409)
		}
		if !live {
			return domain.NewError("SNAPSHOT_EXPIRED", 410)
		}
		if status != nil {
			// Completed public replay belongs to the coordinator, not this phase.
			return domain.NewError("DELIVERY_CREATE_STALE", 409)
		}
		return intent.admitPreparedPackage(ctx, tx, who, tid, saved,pins,watermark)
	})
}
