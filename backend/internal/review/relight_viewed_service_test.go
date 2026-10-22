package review

import (
 "context"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
)
type relightViewedCatalogueStub struct{snapshot CandidateSnapshot;reads int;lookups int}
func (s *relightViewedCatalogueStub) ReadRelightViewedSnapshot(context.Context,auth.Identity,uuid.UUID,uuid.UUID)(CandidateSnapshot,error){s.reads++;return s.snapshot,nil}
func (s *relightViewedCatalogueStub) Lookup(context.Context,auth.Identity,uuid.UUID,uuid.UUID,string,string)(CandidatePreviewObjects,error){s.lookups++;return CandidatePreviewObjects{},nil}
func TestRelightViewedServiceRefusesForeignPairBeforePhysicalIO(t *testing.T){
 now:=time.Date(2026,10,16,12,0,0,0,time.UTC);key:=[]byte(strings.Repeat("k",32));g:=relightViewedFixture(now)
 before,err:=SealPendingPreviewGrant(key,g.Before,now);if err!=nil{t.Fatal(err)}
 after,err:=SealPendingPreviewGrant(key,g.After,now);if err!=nil{t.Fatal(err)}
 for _,lane:=range []string{"actor","source","decided","token","snapshot_tenant","snapshot_review","snapshot_run","cancelled"}{t.Run(lane,func(t *testing.T){
  snapshot:=CandidateSnapshot{TenantID:g.Before.Tenant,ReviewID:g.Before.Review,RunID:uuid.New(),Revision:1,State:"pending",Kind:"image.relight",Base:ArtifactRef{ID:g.Before.Source,SHA256:g.Before.SourceSHA},Candidate:ArtifactRef{ID:g.After.Source,SHA256:g.After.SourceSHA}}
  who:=auth.Identity{UserID:g.Before.User,SessionID:g.Before.Session};b:=before
  switch lane{case "actor":who.UserID=uuid.New();case "source":snapshot.Candidate.ID=uuid.New();case "decided":snapshot.State="decided";case "token":b+="x";case "snapshot_tenant":snapshot.TenantID=uuid.New();case "snapshot_review":snapshot.ReviewID=uuid.New();case "snapshot_run":snapshot.RunID=uuid.Nil}
  catalog:=&relightViewedCatalogueStub{snapshot:snapshot};service:=CandidatePreviewService{Catalog:catalog,Clock:func()time.Time{return now}}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if lane=="cancelled"{cancel()}
  if _,err:=service.AcknowledgeRelightViewed(ctx,who,g.Before.Tenant,g.Before.Review,b,after,key);err==nil{t.Fatal("foreign or stale pair acknowledged")}
  if catalog.lookups!=0{t.Fatal("invalid pair reached physical catalogue")}
  if (lane=="token"||lane=="cancelled")&&catalog.reads!=0{t.Fatal("invalid signature reached SQL snapshot")}
 })}
}
