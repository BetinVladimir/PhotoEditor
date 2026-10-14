package interchange

import (
 "context"
 "errors"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

type ArchiveBeginGenerationTicket struct {
 tenant, upload, generation, writer, reservation uuid.UUID
 actor auth.Identity
 requestSHA, key string
}

func (t ArchiveBeginGenerationTicket) valid() bool {
 if t.tenant == uuid.Nil || t.upload == uuid.Nil || t.generation == uuid.Nil || t.writer == uuid.Nil || t.reservation == uuid.Nil || t.actor.UserID == uuid.Nil || t.actor.SessionID == uuid.Nil || len(t.requestSHA) != 64 { return false }
 for _, digit := range t.requestSHA { if (digit < '0' || digit > '9') && (digit < 'a' || digit > 'f') { return false } }
 key, err := objectstore.InterchangeGenerationKey(t.tenant, t.upload, t.generation)
 return err == nil && t.key == key
}

// SQLArchiveBeginGenerations reads and transitions the durable Begin generation.
type SQLArchiveBeginGenerations struct { DB *pgxpool.Pool; Bucket string }

type ArchiveBeginGenerationSnapshot struct {
 Ticket ArchiveBeginGenerationTicket
 State, MultipartID string
}

// ReadStatus checks the current session without returning a Begin capability.
// A new session of the same actor can inspect an in-flight upload but cannot
// use its creator-session writer ticket to close or bind the generation.
func (s SQLArchiveBeginGenerations) ReadStatus(ctx context.Context, who auth.Identity, tid uuid.UUID, intent ArchiveUploadIntent, reservation uuid.UUID) (*ArchiveBeginGenerationStatus,error) {
 if err := (SQLArchiveUploadRepository{DB:s.DB,Bucket:s.Bucket}).validate(ctx,who,tid,intent); err != nil { return nil,err }
 if reservation == uuid.Nil { return nil,v2input.ErrInvalid }
 var state string;var multipart *string
 err := tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  return tx.QueryRow(ctx,`SELECT state,multipart_id FROM photo.v30_archive_begin_generation_status($1,$2,$3,$4,$5,$6,$7)`,tid,intent.Request().ProjectID,who.UserID,who.SessionID,intent.ID(),reservation,intent.RequestSHA()).Scan(&state,&multipart)
 })
 if errors.Is(err,pgx.ErrNoRows){return nil,nil};if err!=nil{return nil,err}
 result:=&ArchiveBeginGenerationStatus{State:state}
 switch state{
 case "creating":if multipart!=nil{return nil,objectstore.ErrIntegrity}
 case "created","bound","aborting":if multipart==nil||!objectstore.ValidInterchangeMultipartID(*multipart){return nil,objectstore.ErrIntegrity};result.MultipartID=*multipart
 default:return nil,objectstore.ErrIntegrity
 }
 if err:=ctx.Err();err!=nil{return nil,err}
 return result,nil
}

func (s SQLArchiveBeginGenerations) Read(ctx context.Context, who auth.Identity, tid uuid.UUID, intent ArchiveUploadIntent, reservation uuid.UUID) (*ArchiveBeginGenerationSnapshot,error) {
 if err := (SQLArchiveUploadRepository{DB:s.DB,Bucket:s.Bucket}).validate(ctx,who,tid,intent); err != nil { return nil,err }
 if reservation == uuid.Nil { return nil,v2input.ErrInvalid }
 var generation,writer uuid.UUID;var key,state string;var multipart *string
 err := tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  err := tx.QueryRow(ctx,`SELECT generation_id,writer_token,object_key,state,multipart_id FROM photo.read_v30_archive_begin_generation_checked($1,$2,$3,$4,$5,$6)`,tid,who.UserID,who.SessionID,intent.ID(),reservation,intent.RequestSHA()).Scan(&generation,&writer,&key,&state,&multipart)
  return err
 })
 if errors.Is(err,pgx.ErrNoRows){return nil,nil};if err!=nil{return nil,err}
 snapshot:=&ArchiveBeginGenerationSnapshot{Ticket:ArchiveBeginGenerationTicket{tenant:tid,upload:intent.ID(),generation:generation,writer:writer,reservation:reservation,actor:who,requestSHA:intent.RequestSHA(),key:key},State:state}
 if !snapshot.Ticket.valid(){return nil,objectstore.ErrIntegrity}
 switch state{
 case "creating":if multipart!=nil{return nil,objectstore.ErrIntegrity}
 case "created","bound","aborting":if multipart==nil||!objectstore.ValidInterchangeMultipartID(*multipart){return nil,objectstore.ErrIntegrity};snapshot.MultipartID=*multipart
 default:return nil,objectstore.ErrIntegrity
 }
 if err:=ctx.Err();err!=nil{return nil,err}
 return snapshot,nil
}

func (s SQLArchiveBeginGenerations) Start(ctx context.Context, who auth.Identity, tid uuid.UUID, intent ArchiveUploadIntent, reservation uuid.UUID) (ArchiveBeginGenerationTicket, error) {
 var zero ArchiveBeginGenerationTicket
 if err := (SQLArchiveUploadRepository{DB:s.DB,Bucket:s.Bucket}).validate(ctx,who,tid,intent); err != nil { return zero,err }
 if reservation == uuid.Nil { return zero,v2input.ErrInvalid }
 var generation, writer uuid.UUID
 var key string
 err := tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role) error {
  err := tx.QueryRow(ctx,`SELECT generation_id,writer_token,object_key FROM photo.start_v30_archive_begin_generation_checked($1,$2,$3,$4,$5,$6)`,tid,who.UserID,who.SessionID,intent.ID(),reservation,intent.RequestSHA()).Scan(&generation,&writer,&key)
  if errors.Is(err,pgx.ErrNoRows) { return artifactjson.ErrStale }
  return err
 })
 if err != nil { return zero,err }
 ticket := ArchiveBeginGenerationTicket{tenant:tid,upload:intent.ID(),generation:generation,writer:writer,reservation:reservation,actor:who,requestSHA:intent.RequestSHA(),key:key}
 if !ticket.valid() { return zero,objectstore.ErrIntegrity }
 // The SQL transaction may have committed just before ctx was cancelled.
 // Return its ticket so beginGeneration can close the generation with its
 // positive no-dispatch proof before returning the cancellation to the caller.
 return ticket,nil
}

// Close records only a concrete successful Create reply. Unknown transport
// outcomes leave the SQL writer and generation in their creating state.
func (s SQLArchiveBeginGenerations) Close(ctx context.Context, ticket ArchiveBeginGenerationTicket, multipart string) error {
 if err := ctx.Err(); err != nil { return err }
 if s.DB == nil || !ticket.valid() || !objectstore.ValidInterchangeMultipartID(multipart) { return v2input.ErrInvalid }
 return tenant.WithTenant(ctx,s.DB,ticket.actor,ticket.tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role) error {
  var accepted bool
  err := tx.QueryRow(ctx,`SELECT photo.finish_v30_archive_begin_generation($1,$2,$3,$4,$5,$6,$7,$8)`,ticket.tenant,ticket.actor.UserID,ticket.actor.SessionID,ticket.upload,ticket.reservation,ticket.generation,ticket.writer,multipart).Scan(&accepted)
  if err != nil { return err }
  if !accepted { return artifactjson.ErrStale }
  return nil
 })
}

// FailBeforeDispatch is valid only when the caller owns transport evidence
// that S3 Create was never dispatched. It cannot reconcile an unknown reply.
func (s SQLArchiveBeginGenerations) FailBeforeDispatch(ctx context.Context,ticket ArchiveBeginGenerationTicket)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.DB==nil||!ticket.valid(){return v2input.ErrInvalid}
 return tenant.WithTenant(ctx,s.DB,ticket.actor,ticket.tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var accepted bool
  err:=tx.QueryRow(ctx,`SELECT photo.finish_v30_archive_begin_not_dispatched($1,$2,$3,$4,$5,$6,$7)`,ticket.tenant,ticket.actor.UserID,ticket.actor.SessionID,ticket.upload,ticket.reservation,ticket.generation,ticket.writer).Scan(&accepted)
  if err!=nil{return err};if !accepted{return artifactjson.ErrStale};return nil
 })
}

func (s SQLArchiveBeginGenerations) Bind(ctx context.Context, ticket ArchiveBeginGenerationTicket, multipart string) error {
 if err := ctx.Err(); err != nil { return err }
 if s.DB == nil || !ticket.valid() || !objectstore.ValidInterchangeMultipartID(multipart) { return v2input.ErrInvalid }
 return tenant.WithTenant(ctx,s.DB,ticket.actor,ticket.tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role) error {
  var accepted bool
  err := tx.QueryRow(ctx,`SELECT photo.bind_v30_archive_begin_generation($1,$2,$3,$4,$5,$6,$7,$8,$9)`,ticket.tenant,ticket.actor.UserID,ticket.actor.SessionID,ticket.upload,ticket.reservation,ticket.requestSHA,ticket.generation,ticket.writer,multipart).Scan(&accepted)
  if err != nil { return err }
  if !accepted { return artifactjson.ErrStale }
  return nil
 })
}
