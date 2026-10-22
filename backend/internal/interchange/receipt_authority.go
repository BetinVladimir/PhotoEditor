package interchange

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
)

// Raw pending/ticket ledgers use their own FOR SHARE/UPDATE rather than Use.
// Recheck SQL authority after that wait, including callers without tenant.Tx.
func requireReceiptAuthority(ctx context.Context,tx ProofTransaction,who auth.Identity,tid,pid uuid.UUID)error{
 if err:=ctx.Err();err!=nil{return err}
 var allowed bool
 err:=tx.QueryRow(ctx,`SELECT photo.v2_editor_authorized($1,$2,$3,$4) AND EXISTS(SELECT 1 FROM photo.projects p JOIN photo.tenants t ON t.id=p.tenant_id WHERE p.tenant_id=$1 AND p.id=$2 AND p.archived_at IS NULL AND p.download_allowed AND t.status='active')`,tid,pid,who.UserID,who.SessionID).Scan(&allowed)
 if err!=nil{return err}
 if !allowed{return domain.NewError("NOT_FOUND",404)}
 return ctx.Err()
}
