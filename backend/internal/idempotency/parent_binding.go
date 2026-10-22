package idempotency

import (
 "context"
 "errors"

 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/domain"
)

// BindParent runs after Use in the SAME transaction, before response delivery.
// Its receipt lock closes races between identical old request hashes targeting
// different URL parents. It never changes historical fingerprints or re-admits
// old work. Legacy proofs must use persisted resource identity, not expiring
// objects, mutable revisions or a guessed association.
func BindParent(ctx context.Context, tx pgx.Tx, scope Scope, out Response, parent string, proveLegacy func() error) error {
 if err := ctx.Err(); err != nil { return err }
 if len(parent)<1 || len(parent)>256 || out.Status<200 || out.Status>299 { return errors.New("invalid idempotency parent binding") }
 var stored *string
 err := tx.QueryRow(ctx, `SELECT parent_binding FROM photo.idempotency_records
 WHERE tenant_id=$1 AND principal_type=$2 AND principal_id=$3 AND route=$4 AND key=$5 FOR UPDATE`,
 scope.Tenant,scope.PrincipalType,scope.Principal,scope.Route,scope.Key).Scan(&stored)
 if err != nil { return err }
 if err = ctx.Err(); err != nil { return err }
 if stored != nil {
  if *stored != parent { return domain.NewError("IDEMPOTENCY_CONFLICT",409) }
  return nil
 }
 if out.Replayed {
  if proveLegacy==nil { return domain.NewError("IDEMPOTENCY_CONFLICT",409) }
  if err = proveLegacy(); err != nil { return err }
 }
 if err = ctx.Err(); err != nil { return err }
 tag,err := tx.Exec(ctx, `UPDATE photo.idempotency_records SET parent_binding=$6
 WHERE tenant_id=$1 AND principal_type=$2 AND principal_id=$3 AND route=$4 AND key=$5 AND parent_binding IS NULL`,
 scope.Tenant,scope.PrincipalType,scope.Principal,scope.Route,scope.Key,parent)
 if err != nil { return err }
 if tag.RowsAffected()!=1 { return errors.New("idempotency parent binding was not persisted") }
 return ctx.Err()
}
