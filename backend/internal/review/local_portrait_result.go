package review

import (
 "context"
 "errors"
 "time"
 "crypto/sha256"
 "encoding/hex"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/tenant"
)

type LocalPortraitResult struct {
 RunID uuid.UUID `json:"run_id"`
 ResultID uuid.UUID `json:"result_id"`
 Operation string `json:"operation"`
 Base ArtifactRef `json:"base"`
 Candidate ArtifactRef `json:"candidate"`
 ReviewID *uuid.UUID `json:"review_id"`
}
type LocalPortraitAutoAppliedVersion struct {
 AssetID uuid.UUID `json:"asset_id"`
 ProjectID uuid.UUID `json:"project_id"`
 VersionID uuid.UUID `json:"asset_version_id"`
 RunID uuid.UUID `json:"run_id"`
 ResultID uuid.UUID `json:"result_id"`
 Operation string `json:"operation"`
 Source ArtifactRef `json:"source"`
}

// readLocalPortraitResult binds object locators to the live adopted result. No
// caller supplied object locator or candidate document is accepted.
func(s *Service) readLocalPortraitResult(ctx context.Context,who auth.Identity,tid,run uuid.UUID)(LocalPortraitResult,objectstore.Object,objectstore.Object,error){
 var out LocalPortraitResult
 var base,image objectstore.Object
 if s==nil||s.DB==nil{return out,base,image,domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  err:=tx.QueryRow(ctx,`SELECT run_id,result_id,operation,base_id,base_sha,candidate_id,candidate_sha FROM photo.get_v28_local_portrait_result($1,$2,$3,$4)`,tid,who.UserID,who.SessionID,run).Scan(&out.RunID,&out.ResultID,&out.Operation,&out.Base.ID,&out.Base.SHA256,&out.Candidate.ID,&out.Candidate.SHA256)
  if errors.Is(err,pgx.ErrNoRows){return domain.NewError("NOT_FOUND",404)};if err!=nil{return err}
  if !localPortraitKind(out.Operation)||!out.Base.valid()||!out.Candidate.valid(){return domain.NewError("STALE_CANDIDATE_REVIEW",409)}
  for _,pair:=range []struct{ref ArtifactRef;object *objectstore.Object}{{out.Base,&base},{out.Candidate,&image}}{
   var bucket,media string;var size int64
   err=tx.QueryRow(ctx,`SELECT bucket,object_key,object_version,sha256,media_type,bytes FROM photo.artifacts WHERE tenant_id=$1 AND id=$2 AND sha256=$3 AND state='ready' AND verified_at IS NOT NULL AND object_version NOT IN('','null') AND (expires_at IS NULL OR expires_at>clock_timestamp())`,tid,pair.ref.ID,pair.ref.SHA256).Scan(&bucket,&pair.object.Key,&pair.object.VersionID,&pair.object.SHA256,&media,&size)
   if errors.Is(err,pgx.ErrNoRows){return domain.NewError("STALE_CANDIDATE_REVIEW",409)};if err!=nil{return err}
   if bucket!=s.Bucket||media!="application/json"||size<2||size>1048576{return domain.NewError("INPUT_INVALID",422)}
   pair.object.Bytes=size;pair.object.ContentType=media
  }
  return ctx.Err()
 })
 return out,base,image,err
}
func(s *Service) ReadLocalPortraitResult(ctx context.Context,who auth.Identity,tid,run uuid.UUID)(LocalPortraitResult,error){
 out,_,_,err:=s.readLocalPortraitResult(ctx,who,tid,run);if err!=nil{return LocalPortraitResult{},err};if err=ctx.Err();err!=nil{return LocalPortraitResult{},err};return out,nil
}
func(s *Service) ApplyLocalPortraitResult(ctx context.Context,who auth.Identity,tid,run,result uuid.UUID,candidateSHA string)(LocalPortraitAutoAppliedVersion,error){
 var zero LocalPortraitAutoAppliedVersion
 if s==nil||s.Objects==nil{return zero,domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
 if result==uuid.Nil{return zero,domain.NewError("VALIDATION_FAILED",422)}
 if _,err:=domain.ParseDigest(candidateSHA);err!=nil{return zero,domain.NewError("VALIDATION_FAILED",422)}
 ctx,cancel:=context.WithTimeout(ctx,30*time.Second);defer cancel()
 first,baseObject,imageObject,err:=s.readLocalPortraitResult(ctx,who,tid,run);if err!=nil{return zero,err}
 if first.ResultID!=result||first.Candidate.SHA256!=candidateSHA{return zero,domain.NewError("STALE_CANDIDATE_REVIEW",409)}
 baseRaw,err:=readObject(ctx,s.Objects,baseObject);if err!=nil{return zero,err}
 imageRaw,err:=readObject(ctx,s.Objects,imageObject);if err!=nil{return zero,err}
 for _,item:=range []struct{raw []byte;sha string}{{baseRaw,first.Base.SHA256},{imageRaw,first.Candidate.SHA256}}{
  hash:=sha256.Sum256(item.raw);if hex.EncodeToString(hash[:])!=item.sha{return zero,domain.NewError("INPUT_INVALID",422)}
 }
 base,err:=ParseWorkingImage(baseRaw);if err!=nil{return zero,domain.NewError("INPUT_INVALID",422)}
 image,err:=ParseWorkingImage(imageRaw);if err!=nil||!compatibleCandidate(first.Operation,image,base){return zero,domain.NewError("INPUT_INVALID",422)}
 second,b2,i2,err:=s.readLocalPortraitResult(ctx,who,tid,run);if err!=nil{return zero,err}
 if first!=second||baseObject!=b2||imageObject!=i2{return zero,domain.NewError("STALE_CANDIDATE_REVIEW",409)}
 var version *uuid.UUID;var asset,project uuid.UUID
 err=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  if _,e:=tx.Exec(ctx,"SET LOCAL statement_timeout='15s'");e!=nil{return e}
  if e:=tx.QueryRow(ctx,`SELECT photo.apply_v28_local_portrait_auto_version($1,$2,$3,$4,$5,$6,$7,$8)`,tid,run,who.UserID,who.SessionID,result,candidateSHA,string(baseRaw),string(imageRaw)).Scan(&version);e!=nil{return e}
  if version==nil||*version!=uuid.NewSHA1(result,[]byte("V28.LocalPortraitAutoAppliedVersion")){return domain.NewError("STALE_CANDIDATE_REVIEW",409)}
  return tx.QueryRow(ctx,`SELECT v.asset_id,a.project_id FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id WHERE v.tenant_id=$1 AND v.id=$2`,tid,*version).Scan(&asset,&project)
 });if err!=nil{return zero,err};if err=ctx.Err();err!=nil{return zero,err}
 return localPortraitAutoAppliedVersion(first,asset,project,*version),nil
}

// The public run identity comes from the adopted result, never from the
// workflow route identifier used to look up that result.
func localPortraitAutoAppliedVersion(selected LocalPortraitResult,asset,project,version uuid.UUID) LocalPortraitAutoAppliedVersion {
 return LocalPortraitAutoAppliedVersion{asset,project,version,selected.RunID,selected.ResultID,selected.Operation,selected.Candidate}
}
