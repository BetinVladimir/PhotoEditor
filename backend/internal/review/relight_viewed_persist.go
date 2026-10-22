package review

import (
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
)

// SQL owns producer/object locking; this guard rejects unverified DTO state
// and mismatched locked source identity before invoking the SQL decision gate.
func relightDecisionViewedBinding(request Decision,snapshot CandidateSnapshot,actor,session uuid.UUID,now time.Time)bool{
 return request.viewedGrant!=nil&&MatchesRelightViewedGrant(*request.viewedGrant,snapshot.TenantID,snapshot.ReviewID,actor,session,snapshot.Revision,snapshot.Base,snapshot.Candidate,now)
}

// Rebuild trusted state on every call, including reuse of an internal Decision.
// The displayed pair remains an expiring credential, not a new human intent.
func bindRelightViewedDecision(request Decision,key []byte,who auth.Identity,tid,id uuid.UUID,
 kind string,base,candidate ArtifactRef,now time.Time)(Decision,error){
 request.viewedGrant=nil
 if kind!="image.relight"{return request,nil}
 if len(key)<32{return request,domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
 grant,err:=OpenRelightViewedGrant(key,request.ViewedToken,now)
 if err!=nil||!MatchesRelightViewedGrant(grant,tid,id,who.UserID,who.SessionID,request.ExpectedRevision,base,candidate,now){
  return request,domain.NewError("REVIEW_VIEW_REQUIRED",409)
 }
 request.viewedGrant=&grant
 return request,nil
}
