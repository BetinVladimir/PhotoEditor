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

type SQLArchiveOpenBeginStatus struct{DB *pgxpool.Pool}

func(s SQLArchiveOpenBeginStatus)Open(ctx context.Context,who auth.Identity,tid,pid,upload,reservation uuid.UUID)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err}
 if s.DB==nil||tid==uuid.Nil||pid==uuid.Nil||upload==uuid.Nil||reservation==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil{return false,v2input.ErrInvalid}
 var open bool
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  return tx.QueryRow(ctx,`SELECT photo.v30_archive_open_begin_for_status($1,$2,$3,$4,$5,$6)`,tid,pid,who.UserID,who.SessionID,upload,reservation).Scan(&open)
 })
 if err!=nil{return false,err};if err:=ctx.Err();err!=nil{return false,err}
 return open,nil
}
