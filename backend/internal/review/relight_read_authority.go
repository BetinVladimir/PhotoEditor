package review

import (
 "context"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
)

type relightAuthorityReader interface {
 QueryRow(context.Context, string, ...any) pgx.Row
}

// Called inside the tenant transaction on each side of physical media reads.
func requireRelightReadAuthority(ctx context.Context, tx relightAuthorityReader, who auth.Identity, tid, reviewID uuid.UUID, kind string) error {
 if kind != "image.relight" && kind != "image.reflection" && !localPortraitKind(kind) { return nil }
 if err := ctx.Err(); err != nil { return err }
 var ready bool
 if kind == "image.reflection" {
  if err := tx.QueryRow(ctx, `SELECT photo.v28_reflection_current_authority($1,$2,$3,$4)`, tid, reviewID, who.UserID, who.SessionID).Scan(&ready); err != nil { return err }
 } else if localPortraitKind(kind) {
  // A stored adoption receipt alone does not retain live consent, runtime or
  // source authority. Bind the review's exact operation/run on every read.
  if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT FROM photo.reviews v
   WHERE v.tenant_id=$1 AND v.id=$2 AND v.kind=$5
   AND v.kind IN('portrait.hair','portrait.clothing')
   AND v.state IN('pending','decided')
   AND photo.v28_local_portrait_result_ready($1,v.run_id,$3,$4))`, tid, reviewID, who.UserID, who.SessionID, kind).Scan(&ready); err != nil { return err }
 } else if err := tx.QueryRow(ctx, `SELECT photo.v28_relight_review_read_ready($1,$2,$3,$4)`, tid, reviewID, who.UserID, who.SessionID).Scan(&ready); err != nil { return err }
 if !ready { return domain.NewError("STALE_CANDIDATE_REVIEW", 409) }
 return nil
}
