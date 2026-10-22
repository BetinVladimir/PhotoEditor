package workflow

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2render"
)

// No callbacks can stand in for protected admission. Each SQL entry must be
// installed by the exact owning migration; missing helpers fail closed.
type AuthoredFacePreviewRuntime struct { DB *pgxpool.Pool; Objects artifactjson.Objects; Bucket,SRGBSHA,Policy string }
type AuthoredFacePreviewReceipt struct { Run,Task uuid.UUID; State string }
type authoredFacePreviewDocumentRepository struct {
 runtime AuthoredFacePreviewRuntime
 scope artifactjson.Scope
 candidate,image,params string
 ordinal int; parent uuid.UUID; candidateSHA string
}
func(r *authoredFacePreviewDocumentRepository) Reserve(ctx context.Context,s artifactjson.Scope,sha string,size int64)(artifactjson.Reservation,error){
 var result artifactjson.Reservation;var ready bool
 if s!=r.scope||sha!=renderDerivativeBytesSHA([]byte(r.params))||size!=int64(len(r.params)){return result,objectstore.ErrInvalid}
 err:=withPerspectivePreviewDocumentTx(ctx,r.runtime.DB,s.Tenant,func(ctx context.Context,tx pgx.Tx)error{
  return tx.QueryRow(ctx,"SELECT * FROM photo.reserve_authored_face_preview_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)",s.Tenant,r.parent,r.ordinal,r.candidateSHA,s.Run,s.Owner,r.runtime.Bucket,r.candidate,r.image,r.params,size).Scan(&result.ArtifactID,&result.Token,&ready)
 })
 if err!=nil{return result,err};if result.ArtifactID==uuid.Nil||result.Token==uuid.Nil{return result,artifactjson.ErrStale};if ready{result.Ready=&artifactjson.Ref{ID:result.ArtifactID,SHA256:sha}};return result,nil
}
func(r *authoredFacePreviewDocumentRepository) Complete(ctx context.Context,s artifactjson.Scope,ticket artifactjson.Reservation,object objectstore.Object)(artifactjson.Ref,error){
 var zero artifactjson.Ref
 key,err:=artifactjson.DocumentKey(s,ticket.ArtifactID)
 if err!=nil||s!=r.scope||ticket.ArtifactID==uuid.Nil||ticket.Token==uuid.Nil||object.Key!=key||object.SHA256!=renderDerivativeBytesSHA([]byte(r.params))||object.Bytes!=int64(len(r.params))||object.ContentType!="application/json"||object.VersionID==""||object.VersionID=="null"{return zero,objectstore.ErrInvalid}
 var ok bool
 err=withPerspectivePreviewDocumentTx(ctx,r.runtime.DB,s.Tenant,func(ctx context.Context,tx pgx.Tx)error{
  return tx.QueryRow(ctx,"SELECT photo.complete_authored_face_preview_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)",s.Tenant,r.parent,r.ordinal,r.candidateSHA,s.Run,s.Owner,ticket.ArtifactID,ticket.Token,r.candidate,r.image,object.VersionID,object.Bytes,r.runtime.Bucket).Scan(&ok)
 })
 if err!=nil{return zero,err};if !ok{return zero,artifactjson.ErrStale};return artifactjson.Ref{ID:ticket.ArtifactID,SHA256:object.SHA256},nil
}

// Prepare receives owned canonical current WI bytes, then SQL independently
// rechecks the persisted candidate/current producer at every durable boundary.
// It stages a real media.preview task; it never fabricates task output/adoption.
func(s AuthoredFacePreviewRuntime) Prepare(ctx context.Context,candidateSHA string,candidate v2render.AuthoredFaceSelectionCandidate,imageRaw []byte)(AuthoredFacePreviewReceipt,error){
 var zero AuthoredFacePreviewReceipt
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.DB==nil||s.Objects==nil||len(s.Bucket)<3||len(s.Bucket)>63{return zero,objectstore.ErrInvalid}
 candidateRaw,err:=canonical.Marshal(candidate);if err!=nil{return zero,err}
 // Own asynchronous inputs; no caller mutation may change publication bytes.
 var owned v2render.AuthoredFaceSelectionCandidate
 if json.Unmarshal(candidateRaw,&owned)!=nil{return zero,objectstore.ErrInvalid};candidate=owned
 imageRaw=append([]byte(nil),imageRaw...)
 document,err:=CompileAuthoredFacePreviewDocument(candidateSHA,candidate,imageRaw,s.SRGBSHA,s.Policy);if err!=nil{return zero,err}
 scope:=artifactjson.Scope{Tenant:candidate.Tenant,Run:document.Run,Owner:document.ID,Purpose:"task_params"}
 repository:=&authoredFacePreviewDocumentRepository{runtime:s,scope:scope,candidate:string(candidateRaw),image:string(imageRaw),params:string(document.Params),parent:candidate.Parent,ordinal:candidate.Ordinal,candidateSHA:candidateSHA}
 // Begin performs locked current-candidate validation and idempotent quota
 // reservation before any immutable Put. Ordinary model scopes are not reused.
 var admitted bool
 err=withPerspectivePreviewDocumentTx(ctx,s.DB,candidate.Tenant,func(ctx context.Context,tx pgx.Tx)error{
  return tx.QueryRow(ctx,"SELECT photo.begin_authored_face_preview($1,$2,$3,$4,$5,$6,$7,$8,$9)",candidate.Tenant,candidate.Parent,candidate.Ordinal,candidateSHA,document.Run,string(candidateRaw),string(imageRaw),s.SRGBSHA,s.Policy).Scan(&admitted)
 })
 if err!=nil{return zero,err};if !admitted{return zero,artifactjson.ErrStale}
 params,err:=(artifactjson.Publisher{Repository:repository,Objects:s.Objects}).Publish(ctx,scope,document.Params);if err!=nil{return zero,err}
 task,err:=PrepareAuthoredFacePreviewTask(document,review.ArtifactRef{ID:params.ID,SHA256:params.SHA256});if err!=nil{return zero,err}
 taskRaw,err:=canonical.Marshal(task);if err!=nil{return zero,err}
 var state string
 err=withPerspectivePreviewDocumentTx(ctx,s.DB,candidate.Tenant,func(ctx context.Context,tx pgx.Tx)error{
  // One transaction materializes blocked task then seals its one-node graph.
  var staged bool
  if err:=tx.QueryRow(ctx,"SELECT photo.stage_authored_face_preview($1,$2,$3,$4,$5,$6,$7,$8::jsonb)",candidate.Tenant,candidate.Parent,candidate.Ordinal,candidateSHA,document.Run,string(candidateRaw),string(imageRaw),string(taskRaw)).Scan(&staged);err!=nil{return err};if !staged{return artifactjson.ErrStale}
  return tx.QueryRow(ctx,"SELECT photo.seal_authored_face_preview($1,$2,$3,$4,$5)",candidate.Tenant,candidate.Parent,candidate.Ordinal,candidateSHA,document.Run).Scan(&state)
 })
 if err!=nil{return zero,err};switch state{case "running","completed":default:return zero,artifactjson.ErrStale}
 return AuthoredFacePreviewReceipt{Run:document.Run,Task:document.ID,State:state},nil
}
