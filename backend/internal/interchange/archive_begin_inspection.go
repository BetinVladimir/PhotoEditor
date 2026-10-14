package interchange

import (
 "context"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)
type ArchiveBeginDiscovery interface{DiscoverInterchangeMultipart(context.Context,uuid.UUID,uuid.UUID)(string,error)}
type ArchiveBeginInspection struct{WriterToken uuid.UUID;ObservedMultipart string}
type SQLArchiveBeginInspector struct{DB *pgxpool.Pool;Bucket string;Objects ArchiveBeginDiscovery}
// Inspect binds a non-destructive storage observation to a currently open
// writer under authenticated transaction locks. It does NOT prove the original
// Create request has terminated; callers must never use this as CloseBegin.
func(s SQLArchiveBeginInspector)Inspect(ctx context.Context,who auth.Identity,tid uuid.UUID,intent ArchiveUploadIntent,reservation uuid.UUID)(ArchiveBeginInspection,error){
 var out ArchiveBeginInspection
 if err:=ctx.Err();err!=nil{return out,err}
 // Discovery holds authenticated ledger locks; never let storage delay hold
 // them indefinitely. Caller cancellation still takes precedence.
 ctx,stop:=context.WithTimeout(ctx,30*time.Second);defer stop()
 if s.Objects==nil||reservation==uuid.Nil{return out,v2input.ErrInvalid}
 if err:=(SQLArchiveUploadRepository{DB:s.DB,Bucket:s.Bucket}).validate(ctx,who,tid,intent);err!=nil{return out,err}
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  lookup:=func()(*uuid.UUID,error){var token *uuid.UUID;err:=tx.QueryRow(ctx,`SELECT photo.inspect_v30_archive_open_begin($1,$2,$3,$4,$5)`,tid,who.UserID,who.SessionID,intent.ID(),reservation).Scan(&token);return token,err}
  token,err:=lookup();if err!=nil{return err};if token==nil||*token==uuid.Nil{return v2input.ErrInvalid}
  observed,err:=s.Objects.DiscoverInterchangeMultipart(ctx,tid,intent.ID());if err!=nil{return err}
  if err:=ctx.Err();err!=nil{return err}
  if observed!=""&&!objectstore.ValidInterchangeMultipartID(observed){return objectstore.ErrIntegrity}
  current,err:=lookup();if err!=nil{return err};if current==nil||*current!=*token{return objectstore.ErrIntegrity}
  if err=ctx.Err();err!=nil{return err}
  out=ArchiveBeginInspection{WriterToken:*token,ObservedMultipart:observed};return nil
 })
 if err!=nil{return ArchiveBeginInspection{},err};if err:=ctx.Err();err!=nil{return ArchiveBeginInspection{},err};return out,nil
}
