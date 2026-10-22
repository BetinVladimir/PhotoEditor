package v2portrait

import (
 "bytes"
 "context"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

type glareSourceIssuerContext struct {
 SchemaVersion int `json:"schema_version"`
 Tenant uuid.UUID `json:"tenant_id"`
 Run uuid.UUID `json:"run_id"`
 Project uuid.UUID `json:"project_id"`
 Shoot uuid.UUID `json:"shoot_session_id"`
 Actor uuid.UUID `json:"actor_id"`
 AuthSession uuid.UUID `json:"auth_session_id"`
 SourceObservation uuid.UUID `json:"source_observation_id"`
 Params GlareSourceParams `json:"params"`
 ProofJCS string `json:"proof_jcs"`
}

// IssueGlareSourceParams publishes a source-only confirmation in the exact
// existing draft run. The caller must still compile and seal the restoration
// graph; a published confirmation is not a run admission receipt.
func(s OpenEyesConfirmationService)IssueGlareSourceParams(ctx context.Context,who auth.Identity,
 tenant,project,shoot,run uuid.UUID,paramsRaw []byte,observation uuid.UUID)([]byte,error){
 p,err:=ParseGlareSourceParams(paramsRaw);if err!=nil{return nil,err}
 if s.DB==nil||s.Objects==nil||len(s.Bucket)<3||len(s.Bucket)>63||run==uuid.Nil{return nil,v2input.ErrInvalid}
 begin:=func()(pgx.Tx,error){
  if e:=ctx.Err();e!=nil{return nil,e};tx,e:=s.DB.Begin(ctx);if e!=nil{return nil,e};if tx==nil{return nil,v2input.ErrInvalid}
  if _,e=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tenant.String());e!=nil{tx.Rollback(context.WithoutCancel(ctx));return nil,e}
  var role string;e=tx.QueryRow(ctx,"SELECT current_user").Scan(&role)
  if e!=nil||role!="photo_scheduler"{tx.Rollback(context.WithoutCancel(ctx));if e==nil{e=v2input.ErrInvalid};return nil,e};return tx,nil
 }
 tx,err:=begin();if err!=nil{return nil,err}
 body,sha,err:=BuildGlareSourceConfirmation(ctx,tx,who,tenant,project,shoot,paramsRaw,observation)
 tx.Rollback(context.WithoutCancel(ctx));if err!=nil{return nil,err}
 if p.Confirmation.SHA256!=sha{return nil,v2input.ErrExpired}
 p.Confirmation.SHA256=sha
 issuer,err:=canonical.Marshal(glareSourceIssuerContext{1,tenant,run,project,shoot,who.UserID,who.SessionID,observation,p,string(body)});if err!=nil{return nil,err}
 tx,err=begin();if err!=nil{return nil,err}
 var outcome string;var id,token *uuid.UUID;var ready bool
 err=tx.QueryRow(ctx,"SELECT outcome,artifact_id,token,ready FROM photo.reserve_v28_glare_source_document($1,$2,$3,$4::jsonb,$5,$6,$7)",tenant,run,p.Confirmation.ArtifactID,string(issuer),sha,len(body),s.Bucket).Scan(&outcome,&id,&token,&ready)
 if err!=nil{tx.Rollback(context.WithoutCancel(ctx));return nil,err}
 if outcome!="created"&&outcome!="resumed"&&outcome!="replayed"{tx.Rollback(context.WithoutCancel(ctx));switch outcome{case "quota":return nil,artifactjson.ErrQuota;case "conflict":return nil,artifactjson.ErrConflict;default:return nil,artifactjson.ErrStale}}
 if id==nil||token==nil||*id==uuid.Nil||*token==uuid.Nil||ready!=(outcome=="replayed"){tx.Rollback(context.WithoutCancel(ctx));return nil,v2input.ErrInvalid}
 if err=tx.Commit(ctx);err!=nil{return nil,err};if err=ctx.Err();err!=nil{return nil,err}
 for _,ref:=range []v2input.Ref{p.Base.Source,p.GlassesMask,p.Protected,p.SourceFaces,p.Calibration,p.Confirmation}{if ref.ArtifactID==*id{return nil,v2input.ErrInvalid}}
 if !ready{
  key,e:=objectstore.DocumentKey(tenant,run,*id);if e!=nil{return nil,e}
  object,e:=s.Objects.PutImmutable(ctx,key,bytes.NewReader(body),int64(len(body)),sha,"application/json")
  if errors.Is(e,objectstore.ErrExists)||errors.Is(e,objectstore.ErrUnavailable){if e=ctx.Err();e!=nil{return nil,e};object,e=s.Objects.RecoverImmutable(ctx,key,int64(len(body)),sha,"application/json")}
  if e!=nil{return nil,e};if e=ctx.Err();e!=nil{return nil,e}
  if object.Key!=key||object.SHA256!=sha||object.Bytes!=int64(len(body))||object.ContentType!="application/json"||object.VersionID==""||object.VersionID=="null"||len(object.VersionID)>1024{return nil,objectstore.ErrIntegrity}
  tx,e=begin();if e!=nil{return nil,e};var completed bool
  e=tx.QueryRow(ctx,"SELECT photo.complete_v28_glare_source_document($1,$2,$3,$4::jsonb,$5,$6,$7,$8,$9,$10)",tenant,run,p.Confirmation.ArtifactID,string(issuer),*id,*token,sha,len(body),s.Bucket,object.VersionID).Scan(&completed)
  if e!=nil||!completed{tx.Rollback(context.WithoutCancel(ctx));if e==nil{e=artifactjson.ErrStale};return nil,e}
  if e=tx.Commit(ctx);e!=nil{return nil,e}
 }
 if err=ctx.Err();err!=nil{return nil,err}
 p.Confirmation=v2input.Ref{ArtifactID:*id,SHA256:sha};return canonical.Marshal(p)
}
