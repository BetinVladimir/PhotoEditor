package interchange

import (
 "context"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

type SQLArchiveCrossKeyStatus struct{DB *pgxpool.Pool}

func(s SQLArchiveCrossKeyStatus)RequiresReconciliation(ctx context.Context,who auth.Identity,tid uuid.UUID,intent ArchiveUploadIntent,reservation uuid.UUID)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err}
 if s.DB==nil||tid==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||intent.ID()==uuid.Nil||reservation==uuid.Nil{return false,v2input.ErrInvalid}
 var required bool
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  return tx.QueryRow(ctx,`SELECT photo.v30_archive_cross_key_reconciliation_required($1,$2,$3,$4,$5)`,tid,who.UserID,who.SessionID,intent.ID(),reservation).Scan(&required)
 })
 if err!=nil{return false,err};if err:=ctx.Err();err!=nil{return false,err}
 return required,nil
}
