package review

import (
 "context"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
)

type RelightViewedSnapshotReader interface {
 ReadRelightViewedSnapshot(context.Context,auth.Identity,uuid.UUID,uuid.UUID)(CandidateSnapshot,error)
}
type RelightViewedReceipt struct {
 Version int `json:"version"`
 Token string `json:"token"`
 ExpiresAt time.Time `json:"expires_at"`
}

// This reader checks live relight authority in its tenant transaction. The
// decision transaction must repeat authority and token/catalogue validation;
// issuance does not reserve authorization for the lifetime of the receipt.
func (s SQLCandidatePreviewCatalog) ReadRelightViewedSnapshot(ctx context.Context,who auth.Identity,tid,id uuid.UUID)(CandidateSnapshot,error){
 var out CandidateSnapshot
 if s.DB==nil{return out,domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var ready bool
  if err:=tx.QueryRow(ctx,`SELECT photo.v28_relight_review_decision_ready($1,$2,$3,$4)`,tid,id,who.UserID,who.SessionID).Scan(&ready);err!=nil{return err}
  if !ready{return domain.NewError("STALE_CANDIDATE_REVIEW",409)}
  out.TenantID,out.ReviewID=tid,id
  return tx.QueryRow(ctx,`SELECT v.run_id,v.revision,v.state,v.kind,v.candidate_artifact_id,v.candidate_hash,v.base_artifact_id,a.sha256
   FROM photo.reviews v JOIN photo.artifacts a ON a.tenant_id=v.tenant_id AND a.id=v.base_artifact_id
   WHERE v.tenant_id=$1 AND v.id=$2 AND v.kind='image.relight' AND v.state='pending'`,tid,id).Scan(&out.RunID,&out.Revision,&out.State,&out.Kind,&out.Candidate.ID,&out.Candidate.SHA256,&out.Base.ID,&out.Base.SHA256)
 })
 return out,err
}

func (s CandidatePreviewService) AcknowledgeRelightViewed(ctx context.Context,who auth.Identity,tid,id uuid.UUID,beforeToken,afterToken string,key []byte)(RelightViewedReceipt,error){
 var zero RelightViewedReceipt
 if err:=ctx.Err();err!=nil{return zero,err}
 if tid==uuid.Nil||id==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil{return zero,domain.NewError("VALIDATION_FAILED",422)}
 reader,ok:=s.Catalog.(RelightViewedSnapshotReader)
 if !ok||len(key)<32{return zero,domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
 now:=time.Now;if s.Clock!=nil{now=s.Clock}
 before,err:=OpenPendingPreviewGrant(key,beforeToken,now());if err!=nil{return zero,domain.NewError("VALIDATION_FAILED",422)}
 after,err:=OpenPendingPreviewGrant(key,afterToken,now());if err!=nil{return zero,domain.NewError("VALIDATION_FAILED",422)}
 first,err:=reader.ReadRelightViewedSnapshot(ctx,who,tid,id);if err!=nil{return zero,err}
 if first.TenantID!=tid||first.ReviewID!=id||first.RunID==uuid.Nil||first.Kind!="image.relight"||first.State!="pending"||!MatchesRelightViewedGrant(RelightViewedGrant{Version:1,Kind:first.Kind,Revision:first.Revision,Before:before,After:after,ExpiresUnix:min(before.ExpiresUnix,after.ExpiresUnix)},tid,id,who.UserID,who.SessionID,first.Revision,first.Base,first.Candidate,now()){
  return zero,domain.NewError("STALE_CANDIDATE_REVIEW",409)
 }
 // Existing reader verifies physical bytes, exact object version and current
 // source/catalogue before and after IO. Do not replace it with MAC-only checks.
 for _,token:=range []string{beforeToken,afterToken}{
  if _,_,err:=s.ReadPendingPreview(ctx,key,token,now());err!=nil{return zero,err}
 }
 for _,grant:=range []PendingPreviewGrant{before,after}{
  objects,err:=s.Catalog.Lookup(ctx,who,tid,id,grant.Side,grant.Variant);if err!=nil{return zero,err}
  if objects.Source!=(ArtifactRef{ID:grant.Source,SHA256:grant.SourceSHA})||!pendingGrantObjectMatches(grant,objects.Derivative,objects.DerivativeObject,52*1048576){return zero,domain.NewError("STALE_CANDIDATE_REVIEW",409)}
 }
 second,err:=reader.ReadRelightViewedSnapshot(ctx,who,tid,id);if err!=nil{return zero,err}
 if first!=second{return zero,domain.NewError("STALE_CANDIDATE_REVIEW",409)}
 if err:=ctx.Err();err!=nil{return zero,err}
 token,err:=IssueRelightViewedAcknowledgement(key,beforeToken,afterToken,tid,id,who.UserID,who.SessionID,second.Revision,second.Base,second.Candidate,now());if err!=nil{return zero,err}
 grant,err:=OpenRelightViewedGrant(key,token,now());if err!=nil{return zero,err}
 return RelightViewedReceipt{Version:1,Token:token,ExpiresAt:time.Unix(grant.ExpiresUnix,0).UTC()},nil
}
