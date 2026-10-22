package review

import (
 "encoding/json"
 "strings"
 "photoeditor/backend/internal/auth"
 "testing"
 "time"
 "github.com/google/uuid"
)
func TestRelightDecisionRequiresInternallyVerifiedViewedGrant(t *testing.T){
 now:=time.Date(2026,10,16,12,0,0,0,time.UTC);g:=relightViewedFixture(now)
 snapshot:=CandidateSnapshot{TenantID:g.Before.Tenant,ReviewID:g.Before.Review,Revision:1,Base:ArtifactRef{ID:g.Before.Source,SHA256:g.Before.SourceSHA},Candidate:ArtifactRef{ID:g.After.Source,SHA256:g.After.SourceSHA}}
 request:=Decision{ViewedToken:"untrusted-client-text"}
 if relightDecisionViewedBinding(request,snapshot,g.Before.User,g.Before.Session,now){t.Fatal("unverified token text bypassed signature")}
 request.viewedGrant=&g
 if !relightDecisionViewedBinding(request,snapshot,g.Before.User,g.Before.Session,now){t.Fatal("verified exact pair rejected")}
 for _,change:=range []func(*CandidateSnapshot){func(s *CandidateSnapshot){s.TenantID=uuid.New()},func(s *CandidateSnapshot){s.ReviewID=uuid.New()},func(s *CandidateSnapshot){s.Revision++},func(s *CandidateSnapshot){s.Base.ID=uuid.New()},func(s *CandidateSnapshot){s.Candidate.ID=uuid.New()}}{
  changed:=snapshot;change(&changed);if relightDecisionViewedBinding(request,changed,g.Before.User,g.Before.Session,now){t.Fatal("locked source mismatch accepted")}
 }
 if relightDecisionViewedBinding(request,snapshot,g.Before.User,g.Before.Session,now.Add(time.Minute)){t.Fatal("expired grant accepted")}
 raw,err:=json.Marshal(request);if err!=nil{t.Fatal(err)}
 var decoded Decision;if err:=json.Unmarshal(raw,&decoded);err!=nil{t.Fatal(err)}
 if decoded.viewedGrant!=nil{t.Fatal("private trusted state crossed JSON boundary")}
}

func TestRelightDecisionRebuildsTrustedCredentialForEachRequest(t *testing.T){
 now:=time.Date(2026,10,16,12,0,0,0,time.UTC);g:=relightViewedFixture(now);key:=[]byte(strings.Repeat("k",32))
 token,err:=SealRelightViewedGrant(key,g,now);if err!=nil{t.Fatal(err)}
 who:=auth.Identity{UserID:g.Before.User,SessionID:g.Before.Session}
 base:=ArtifactRef{ID:g.Before.Source,SHA256:g.Before.SourceSHA};candidate:=ArtifactRef{ID:g.After.Source,SHA256:g.After.SourceSHA}
 for _,mode:=range []string{"valid","forged","expired","actor","revision","missing_key","other_kind"}{t.Run(mode,func(t *testing.T){
  request:=Decision{ExpectedRevision:1,ViewedToken:token,viewedGrant:&g};actor:=who;clock:=now;secret:=key;kind:="image.relight"
  switch mode{case "forged":request.ViewedToken+="x";case "expired":clock=now.Add(3*time.Minute);case "actor":actor.UserID=uuid.New();case "revision":request.ExpectedRevision=2;case "missing_key":secret=nil;case "other_kind":kind="render.compose"}
  bound,e:=bindRelightViewedDecision(request,secret,actor,g.Before.Tenant,g.Before.Review,kind,base,candidate,clock)
  if mode=="valid"{if e!=nil||bound.viewedGrant==nil{t.Fatal("valid credential rejected",e)}
  }else if mode=="other_kind"{if e!=nil||bound.viewedGrant!=nil{t.Fatal("trusted relight state leaked to another operation")}
  }else if e==nil||bound.viewedGrant!=nil{t.Fatal("unverified reused credential accepted")}
 })}
}
