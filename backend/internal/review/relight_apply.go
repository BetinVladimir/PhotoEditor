package review

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/project"
)

type RelightAppliedVersion struct {
 AssetID uuid.UUID `json:"asset_id"`
 ProjectID uuid.UUID `json:"project_id"`
 VersionID uuid.UUID `json:"asset_version_id"`
 ReviewID uuid.UUID `json:"review_id"`
 DecisionHash string `json:"decision_hash"`
 Source ArtifactRef `json:"source"`
}

// validateRelightApplyImages checks original bytes without reserializing them:
// the durable artifact hash is over those exact bytes, not parsed JSON.
func validateRelightApplyImages(selected RestorationResultSelection,baseRaw,imageRaw []byte) error {
 if selected.SchemaVersion!=1||selected.ReviewID==uuid.Nil||selected.Image==nil||!selected.Image.valid()||
  (selected.Route!="candidate"&&selected.Route!="base") {return domain.NewError("STALE_CANDIDATE_REVIEW",409)}
 if _,err:=domain.ParseDigest(selected.DecisionHash);err!=nil{return domain.NewError("VALIDATION_FAILED",422)}
 sum:=sha256.Sum256(imageRaw)
 if hex.EncodeToString(sum[:])!=selected.Image.SHA256{return domain.NewError("STALE_CANDIDATE_REVIEW",409)}
 base,err:=ParseWorkingImage(baseRaw);if err!=nil{return domain.NewError("INPUT_INVALID",422)}
 image,err:=ParseWorkingImage(imageRaw);if err!=nil{return domain.NewError("INPUT_INVALID",422)}
 if selected.Route=="candidate"&&!compatibleCandidate("image.relight",image,base){return domain.NewError("INPUT_INVALID",422)}
 if selected.Route=="base"&&string(baseRaw)!=string(imageRaw){return domain.NewError("INPUT_INVALID",422)}
 return nil
}

// MaterializeRelightSelection is the persistence boundary used after exact-version
// source reads. SQL repeats human selection and live authority under locks. This
// method neither accepts a new review decision nor runs image processing.
func (s *Service) MaterializeRelightSelection(ctx context.Context,who auth.Identity,tid,id uuid.UUID,
 decisionHash string,baseRaw,imageRaw []byte)(RelightAppliedVersion,error){
 var zero RelightAppliedVersion
 if err:=ctx.Err();err!=nil{return zero,err}
 selected,err:=s.ReadRestorationResultSelection(ctx,who,tid,id);if err!=nil{return zero,err}
 if selected.DecisionHash!=decisionHash{return zero,domain.NewError("STALE_CANDIDATE_REVIEW",409)}
 if err=validateRelightApplyImages(selected,baseRaw,imageRaw);err!=nil{return zero,err}
 bounded,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 var version *uuid.UUID
 var assetID,projectID uuid.UUID
 err=tenant.WithTenant(bounded,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  if _,e:=tx.Exec(bounded,"SET LOCAL statement_timeout='15s'");e!=nil{return e}
  if e:=tx.QueryRow(bounded,`SELECT photo.apply_v28_relight_version($1,$2,$3,$4,$5,$6,$7)`,
   tid,id,who.UserID,who.SessionID,decisionHash,string(baseRaw),string(imageRaw)).Scan(&version);e!=nil{return e}
  if version==nil||*version==uuid.Nil{return domain.NewError("STALE_CANDIDATE_REVIEW",409)}
  if selected.Route=="candidate"&&*version!=uuid.NewSHA1(id,[]byte("V28.RelightAppliedVersion")){
   return domain.NewError("STALE_CANDIDATE_REVIEW",409)
  }
  if e:=tx.QueryRow(bounded,`SELECT v.asset_id,a.project_id FROM photo.asset_versions v JOIN photo.assets a
   ON a.tenant_id=v.tenant_id AND a.id=v.asset_id WHERE v.tenant_id=$1 AND v.id=$2`,tid,*version).Scan(&assetID,&projectID);e!=nil{return e}
  return bounded.Err()
 })
 if cancellation:=bounded.Err();cancellation!=nil{return zero,cancellation}
 if err!=nil{return zero,err}
 return RelightAppliedVersion{AssetID:assetID,ProjectID:projectID,VersionID:*version,ReviewID:id,DecisionHash:decisionHash,Source:*selected.Image},nil
}

// ApplyRelightSelected reads the immutable WorkingImage objects from storage;
// clients supply only the decision hash and cannot inject image documents.
func (s *Service) ApplyRelightSelected(ctx context.Context,who auth.Identity,tid,id uuid.UUID,decisionHash string)(RelightAppliedVersion,error){
 var zero RelightAppliedVersion
 if s==nil||s.DB==nil||s.Objects==nil{return zero,domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
 bounded,cancel:=context.WithTimeout(ctx,30*time.Second);defer cancel()
 bounded=context.WithValue(bounded,allowDecidedReviewContextKey{},true)
 selected,err:=s.ReadRestorationResultSelection(bounded,who,tid,id);if err!=nil{return zero,err}
 if selected.DecisionHash!=decisionHash||selected.Image==nil{return zero,domain.NewError("STALE_CANDIDATE_REVIEW",409)}
 load:=func()(decisionContext,error){
  var value decisionContext
  err:=tenant.WithTenant(bounded,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
   var e error;value,e=loadDecisionContext(bounded,tx,tid,id,s.Bucket);if e!=nil{return e}
   if value.Kind!="image.relight"{return domain.NewError("INPUT_INVALID",422)}
   _,e=project.Authorize(bounded,tx,tid,value.ProjectID,who.UserID,role,domain.Editor,false);return e
  });return value,err
 }
 first,err:=load();if err!=nil{return zero,err}
 expected:=first.Proposal;if selected.Route=="base"{expected=first.Image}
 if *selected.Image!=expected{return zero,domain.NewError("STALE_CANDIDATE_REVIEW",409)}
 baseRaw,err:=readObject(bounded,s.Objects,first.ImageObject);if err!=nil{return zero,err}
 imageRaw:=baseRaw
 if selected.Route=="candidate"{imageRaw,err=readObject(bounded,s.Objects,first.ProposalObject);if err!=nil{return zero,err}}
 second,err:=load();if err!=nil{return zero,err}
 if !sameContext(first,second){return zero,domain.NewError("STALE_CANDIDATE_REVIEW",409)}
 return s.MaterializeRelightSelection(bounded,who,tid,id,decisionHash,baseRaw,imageRaw)
}
