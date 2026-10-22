package review

import (
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
)

func relightViewedFixture(now time.Time) RelightViewedGrant {
 b:=PendingPreviewGrant{Version:1,Tenant:uuid.New(),Review:uuid.New(),User:uuid.New(),Session:uuid.New(),Side:"before",Variant:"preview1600",Source:uuid.New(),SourceSHA:strings.Repeat("a",64),Role:"image",Artifact:uuid.New(),SHA256:strings.Repeat("b",64),ObjectKeySHA:strings.Repeat("c",64),ObjectVersion:"v1",ExpiresUnix:now.Add(time.Minute).Unix()}
 a:=b;a.Side="after";a.Source=uuid.New();a.Artifact=uuid.New();a.SourceSHA=strings.Repeat("d",64)
 return RelightViewedGrant{Version:1,Kind:"image.relight",Revision:1,Before:b,After:a,ExpiresUnix:b.ExpiresUnix}
}
func TestRelightViewedGrantRoundTripAndScope(t *testing.T){
 now:=time.Date(2026,10,16,12,0,0,0,time.UTC);key:=[]byte(strings.Repeat("k",32));g:=relightViewedFixture(now)
 token,err:=SealRelightViewedGrant(key,g,now);if err!=nil{t.Fatal(err)}
 got,err:=OpenRelightViewedGrant(key,token,now);if err!=nil||got!=g{t.Fatal("pair binding lost",err)}
 base:=ArtifactRef{ID:g.Before.Source,SHA256:g.Before.SourceSHA};candidate:=ArtifactRef{ID:g.After.Source,SHA256:g.After.SourceSHA}
 if !MatchesRelightViewedGrant(g,g.Before.Tenant,g.Before.Review,g.Before.User,g.Before.Session,1,base,candidate,now){t.Fatal("valid scope rejected")}
 if MatchesRelightViewedGrant(g,g.Before.Tenant,g.Before.Review,uuid.New(),g.Before.Session,1,base,candidate,now)||MatchesRelightViewedGrant(g,g.Before.Tenant,g.Before.Review,g.Before.User,uuid.New(),1,base,candidate,now)||MatchesRelightViewedGrant(g,g.Before.Tenant,g.Before.Review,g.Before.User,g.Before.Session,2,base,candidate,now){t.Fatal("foreign scope accepted")}
 if MatchesRelightViewedGrant(g,g.Before.Tenant,g.Before.Review,g.Before.User,g.Before.Session,1,candidate,base,now){t.Fatal("swapped sources accepted")}
 if _,err:=OpenRelightViewedGrant(key,token,now.Add(time.Minute));err==nil{t.Fatal("expired pair accepted")}
 if _,err:=OpenPendingPreviewGrant(key,token,now);err==nil{t.Fatal("view acknowledgement became media capability")}
 if _,err:=OpenRelightViewedGrant([]byte(strings.Repeat("x",32)),token,now);err==nil{t.Fatal("foreign signing key accepted")}
}
func TestRelightViewedGrantRejectsMixedPair(t *testing.T){
 now:=time.Date(2026,10,16,12,0,0,0,time.UTC);key:=[]byte(strings.Repeat("k",32))
 cases:=map[string]func(*RelightViewedGrant){
  "tenant":func(g *RelightViewedGrant){g.After.Tenant=uuid.New()},
  "review":func(g *RelightViewedGrant){g.After.Review=uuid.New()},
  "user":func(g *RelightViewedGrant){g.After.User=uuid.New()},
  "session":func(g *RelightViewedGrant){g.After.Session=uuid.New()},
  "variant":func(g *RelightViewedGrant){g.After.Variant="preview2560"},
  "side":func(g *RelightViewedGrant){g.After.Side="before"},
  "manifest":func(g *RelightViewedGrant){g.After.Role="manifest"},
  "source":func(g *RelightViewedGrant){g.After.Source=g.Before.Source},
  "expiry":func(g *RelightViewedGrant){g.ExpiresUnix=g.Before.ExpiresUnix+1},
  "revision":func(g *RelightViewedGrant){g.Revision=2},
  "kind":func(g *RelightViewedGrant){g.Kind="render.compose"},
 }
 for name,mutate:=range cases{t.Run(name,func(t *testing.T){g:=relightViewedFixture(now);mutate(&g);if _,err:=SealRelightViewedGrant(key,g,now);err==nil{t.Fatal("mixed or invalid pair accepted")}})}
}

func TestRelightViewedAcknowledgementRequiresAuthenticImagePair(t *testing.T){
 now:=time.Date(2026,10,16,12,0,0,0,time.UTC);key:=[]byte(strings.Repeat("k",32));g:=relightViewedFixture(now)
 g.After.ExpiresUnix=now.Add(30*time.Second).Unix()
 b,err:=SealPendingPreviewGrant(key,g.Before,now);if err!=nil{t.Fatal(err)}
 a,err:=SealPendingPreviewGrant(key,g.After,now);if err!=nil{t.Fatal(err)}
 base:=ArtifactRef{ID:g.Before.Source,SHA256:g.Before.SourceSHA};candidate:=ArtifactRef{ID:g.After.Source,SHA256:g.After.SourceSHA}
 issue:=func(before,after string,user uuid.UUID)(string,error){return IssueRelightViewedAcknowledgement(key,before,after,g.Before.Tenant,g.Before.Review,user,g.Before.Session,1,base,candidate,now)}
 token,err:=issue(b,a,g.Before.User);if err!=nil{t.Fatal(err)}
 receipt,err:=OpenRelightViewedGrant(key,token,now);if err!=nil||receipt.ExpiresUnix!=g.After.ExpiresUnix{t.Fatal("shorter image expiry not preserved",err)}
 for _,pair:=range [][2]string{{a,b},{b,a+"x"},{b,b},{token,a}}{if _,err:=issue(pair[0],pair[1],g.Before.User);err==nil{t.Fatal("invalid pair accepted")}}
 if _,err:=issue(b,a,uuid.New());err==nil{t.Fatal("foreign actor accepted")}
}
