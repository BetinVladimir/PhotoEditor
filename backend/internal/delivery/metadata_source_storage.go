package delivery

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

// MetadataSourceRepository only publishes the prepared normalized document.
// Construct with a photo_api pool; each SQL call commits before object IO.
type MetadataSourceRepository struct {
 db ProofTransaction
 scope MetadataSourcePublicationScope
 run uuid.UUID
 plan MetadataSourcePublication
 policy string
 approved []string
 bucket string
 currentSession uuid.UUID
}
func NewMetadataSourceRepository(db MetadataTransactionBeginner,scope MetadataSourcePublicationScope,run uuid.UUID,
 plan MetadataSourcePublication,policy string,approved []string,bucket string)(MetadataSourceRepository,error){
 var zero MetadataSourceRepository
 if db==nil||run==uuid.Nil||len(bucket)<3||len(bucket)>63{return zero,v2input.ErrInvalid}
 id,err:=MetadataSourceIdentity(run,scope.Binding,policy,approved)
 if err!=nil||id!=plan.Ref().ArtifactID{return zero,v2input.ErrInvalid}
 owned,err:=RestoreMetadataSourcePublication(plan.JCS(),plan.SHA256(),scope,plan.Ref(),plan.SourceJCS(),policy,approved)
 if err!=nil{return zero,err}
 return MetadataSourceRepository{db:metadataSourceQueries{db,scope.Tenant},scope:scope,run:run,plan:owned,policy:policy,approved:append([]string{},approved...),bucket:bucket},nil
}
func(s MetadataSourceRepository)publicationScope()artifactjson.Scope{
 return artifactjson.Scope{Tenant:s.scope.Tenant,Run:s.run,Owner:s.plan.Ref().ArtifactID,Purpose:"v30_metadata_source"}
}
func(s MetadataSourceRepository)validate(scope artifactjson.Scope)error{
 if s.db==nil||s.run==uuid.Nil||scope!=s.publicationScope()||len(s.bucket)<3||len(s.bucket)>63{return objectstore.ErrInvalid}
 id,err:=MetadataSourceIdentity(s.run,s.scope.Binding,s.policy,s.approved)
 if err!=nil||id!=s.plan.Ref().ArtifactID{return objectstore.ErrInvalid}
 _,err=RestoreMetadataSourcePublication(s.plan.JCS(),s.plan.SHA256(),s.scope,s.plan.Ref(),s.plan.SourceJCS(),s.policy,s.approved)
 return err
}
// WithCurrentSession changes only SQL authorization, never immutable evidence.
func(s MetadataSourceRepository)WithCurrentSession(session uuid.UUID)(MetadataSourceRepository,error){
 if session==uuid.Nil{return MetadataSourceRepository{},v2input.ErrInvalid}
 if err:=s.validate(s.publicationScope());err!=nil{return MetadataSourceRepository{},err}
 s.currentSession=session;return s,nil
}
func(s MetadataSourceRepository)args()[]any{
 session:=s.currentSession;if session==uuid.Nil{session=s.scope.Session}
 return []any{s.scope.Tenant,s.run,s.scope.Actor,session,string(s.plan.JCS()),s.plan.SHA256(),s.policy,append([]string{},s.approved...),s.bucket}
}
func(s MetadataSourceRepository)Reserve(ctx context.Context,scope artifactjson.Scope,sha string,size int64)(artifactjson.Reservation,error){
 var zero artifactjson.Reservation
 if err:=ctx.Err();err!=nil{return zero,err}
 if err:=s.validate(scope);err!=nil{return zero,err}
 ref:=s.plan.Ref()
 if sha!=ref.SHA256||size!=int64(len(s.plan.SourceJCS())){return zero,objectstore.ErrInvalid}
 var outcome string;var id,token *uuid.UUID;var ready bool
 err:=s.db.QueryRow(ctx,`SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_metadata_source($1,$2,$3,$4,$5,$6,$7,$8,$9)`,s.args()...).Scan(&outcome,&id,&token,&ready)
 if err!=nil{return zero,err}
 if err=ctx.Err();err!=nil{return zero,err}
 switch outcome{
 case "created","resumed","replayed":
 case "quota":return zero,artifactjson.ErrQuota
 case "conflict":return zero,artifactjson.ErrConflict
 case "stale","forbidden":return zero,artifactjson.ErrStale
 default:return zero,objectstore.ErrIntegrity
 }
 if id==nil||*id!=ref.ArtifactID||token==nil||*token==uuid.Nil||ready!=(outcome=="replayed"){return zero,objectstore.ErrIntegrity}
 result:=artifactjson.Reservation{ArtifactID:*id,Token:*token}
 if ready{result.Ready=&artifactjson.Ref{ID:*id,SHA256:ref.SHA256}}
 return result,nil
}
func(s MetadataSourceRepository)Complete(ctx context.Context,scope artifactjson.Scope,reservation artifactjson.Reservation,object objectstore.Object)(artifactjson.Ref,error){
 var zero artifactjson.Ref
 if err:=ctx.Err();err!=nil{return zero,err}
 if err:=s.validate(scope);err!=nil{return zero,err}
 ref:=s.plan.Ref();key,err:=objectstore.DocumentKey(s.scope.Tenant,s.run,ref.ArtifactID)
 if err!=nil||reservation.ArtifactID!=ref.ArtifactID||reservation.Token==uuid.Nil||reservation.Ready!=nil||
 object.Key!=key||object.SHA256!=ref.SHA256||object.Bytes!=int64(len(s.plan.SourceJCS()))||
 object.ContentType!="application/json"||!objectstore.ValidInterchangeVersionID(object.VersionID){return zero,objectstore.ErrInvalid}
 args:=append(s.args(),reservation.Token,object.VersionID)
 var accepted bool
 err=s.db.QueryRow(ctx,`SELECT photo.complete_v30_metadata_source($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,args...).Scan(&accepted)
 if err!=nil{return zero,err}
 if err=ctx.Err();err!=nil{return zero,err}
 if !accepted{return zero,artifactjson.ErrStale}
 return artifactjson.Ref{ID:ref.ArtifactID,SHA256:ref.SHA256},nil
}
func PublishMetadataSource(ctx context.Context,repository MetadataSourceRepository,objects artifactjson.Objects)(artifactjson.Ref,error){
 scope:=repository.publicationScope()
 if err:=repository.validate(scope);err!=nil{return artifactjson.Ref{},err}
 return (artifactjson.Publisher{Repository:repository,Objects:objects}).Publish(ctx,scope,repository.plan.SourceJCS())
}
