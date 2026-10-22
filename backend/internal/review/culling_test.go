package review

import (
 "encoding/json"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)
func cullingID(n byte)uuid.UUID{var id uuid.UUID;id[6]=0x40;id[8]=0x80;id[15]=n;return id}
func cullingFixture()(cullingContext,CullingDecision){
 sha:=strings.Repeat("a",64)
 c:=cullingContext{View:View{ID:cullingID(1),RunID:cullingID(2),Kind:"culling",State:"pending",Revision:1,CandidateHash:sha},Snapshot:cullingID(3),Digest:sha,Projection:cullingProjection{Groups:ArtifactRef{cullingID(4),sha},Proposal:ArtifactRef{cullingID(5),sha},NeverDelete:true,Items:[]CullingItem{{ItemID:cullingID(10),AssetID:cullingID(11),VersionID:cullingID(12),SceneID:"scene",ExactGroupID:"exact",BurstGroupID:"burst",Rank:1,Score:.8,Recommended:true,EyeReview:true,EyeState:"unknown"}}}}
 req:=CullingDecision{ExpectedRevision:1,CandidateHash:sha,Selected:[]uuid.UUID{cullingID(10)},Confirmed:[]uuid.UUID{cullingID(10)},NeverDelete:true};return c,req
}
func TestCullingPlanMatchesActorBoundCanonicalWire(t *testing.T){
 c,req:=cullingFixture();selected,hash,e:=planCullingDecision(cullingID(6),cullingID(7),c,req);if e!=nil{t.Fatal(e)}
 expected,e:=canonical.Hash(map[string]any{"version":"culling_decision_v1","tenant_id":cullingID(6).String(),"review_id":c.View.ID.String(),"run_id":c.View.RunID.String(),"actor_id":cullingID(7).String(),"candidate_hash":c.View.CandidateHash,"snapshot_id":c.Snapshot.String(),"snapshot_digest":c.Digest,"selected_item_ids":[]string{cullingID(10).String()},"confirmed_eyes_item_ids":[]string{cullingID(10).String()},"never_delete":true})
 if e!=nil||hash!=expected||!selected.NeverDelete||len(selected.Selected)!=1{t.Fatal("wire/selection mismatch",e)}
 _,other,e:=planCullingDecision(cullingID(6),cullingID(8),c,req);if e!=nil||other==hash{t.Fatal("decision is not actor bound")}
}
func TestCullingPlanRejectsMissingEyesForeignDuplicatesAndStaleReview(t *testing.T){
 cases:=map[string]func(*cullingContext,*CullingDecision){
  "no choice":func(c *cullingContext,r *CullingDecision){r.Selected=[]uuid.UUID{}},
  "missing eyes":func(c *cullingContext,r *CullingDecision){r.Confirmed=[]uuid.UUID{}},
  "foreign":func(c *cullingContext,r *CullingDecision){r.Selected=[]uuid.UUID{cullingID(99)}},
  "foreign confirmation":func(c *cullingContext,r *CullingDecision){r.Confirmed=[]uuid.UUID{cullingID(99)}},
  "duplicate":func(c *cullingContext,r *CullingDecision){r.Selected=append(r.Selected,r.Selected[0])},
  "delete":func(c *cullingContext,r *CullingDecision){r.NeverDelete=false},
  "stale":func(c *cullingContext,r *CullingDecision){r.ExpectedRevision=2},
  "other proposal":func(c *cullingContext,r *CullingDecision){r.CandidateHash=strings.Repeat("b",64)},
  "decided":func(c *cullingContext,r *CullingDecision){c.View.State="decided"},
  "overflow":func(c *cullingContext,r *CullingDecision){r.ExpectedRevision=maxRevision},
 }
 for name,change:=range cases{t.Run(name,func(t *testing.T){c,r:=cullingFixture();change(&c,&r);if _,_,e:=planCullingDecision(cullingID(6),cullingID(7),c,r);e==nil{t.Fatal("invalid selection admitted")}})}
}
func TestCullingProjectionStrictAndSourceBound(t *testing.T){
 c,_:=cullingFixture();raw,_:=json.Marshal(c.Projection);if _,e:=readCullingProjection(raw,c.View.CandidateHash);e!=nil{t.Fatal(e)}
 for _,patch:=range []func(map[string]any){
  func(p map[string]any){p["extra"]=true},func(p map[string]any){p["never_delete"]=false},func(p map[string]any){p["groups"]=nil},func(p map[string]any){p["items"]=[]any{}},
 }{var body map[string]any;json.Unmarshal(raw,&body);patch(body);bad,_:=json.Marshal(body);if _,e:=readCullingProjection(bad,c.View.CandidateHash);e==nil{t.Fatal("invalid projection admitted")}}
 if _,e:=readCullingProjection(raw,strings.Repeat("b",64));e==nil{t.Fatal("rebound proposal admitted")}
 c.Projection.Items=append(c.Projection.Items,c.Projection.Items[0]);raw,_=json.Marshal(c.Projection);if _,e:=readCullingProjection(raw,c.View.CandidateHash);e==nil{t.Fatal("duplicate inventory admitted")}
}
