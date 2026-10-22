package v2region

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)

type authoredMemoryStore struct{ record AuthoredRegionRecord; reads int; readAt func(int,*AuthoredRegionRecord); cancellation bool }
func(s *authoredMemoryStore)Begin(context.Context,AuthoredRegionIntent,string)(AuthoredRegionRecord,error){return s.record,nil}
func(s *authoredMemoryStore)Read(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID)(AuthoredRegionRecord,error){s.reads++;if s.readAt!=nil{s.readAt(s.reads,&s.record)};return s.record,nil}
func(s *authoredMemoryStore)Cancel(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID)error{s.cancellation=true;s.record.CancelRequested=true;s.record.Revision++;return nil}
type authoredMemoryChildren struct{facts AuthoredRegionChildFacts; starts,crops,cancels int; startErr error; inspectAt func(int); inspections int}
func(c *authoredMemoryChildren)Inspect(context.Context,AuthoredRegionRecord)(AuthoredRegionChildFacts,error){c.inspections++;if c.inspectAt!=nil{c.inspectAt(c.inspections)};return c.facts,nil}
func(c *authoredMemoryChildren)StartRender(context.Context,AuthoredRegionRecord)error{c.starts++;c.facts.RenderExists=true;return c.startErr}
func(c *authoredMemoryChildren)StartCrop(context.Context,AuthoredRegionRecord)error{c.crops++;c.facts.CropExists=true;return nil}
func(c *authoredMemoryChildren)Cancel(context.Context,AuthoredRegionRecord)error{c.cancels++;return nil}
func authoredCoordinatorFixture()(*authoredMemoryStore,*authoredMemoryChildren,AuthoredRegionCoordinator){
 i:=authoredRegionIntentFixture();sha,_:=i.SHA256();id,render,crop,_:=authoredRegionIDs(i,"loupe-test")
 store:=&authoredMemoryStore{record:AuthoredRegionRecord{ID:id,Intent:i,SHA:sha,RenderBatch:render,CropRun:crop,Revision:1,Current:true}}
 children:=&authoredMemoryChildren{facts:AuthoredRegionChildFacts{AllWritersClosed:true,AllQuotaSettled:true}}
 return store,children,AuthoredRegionCoordinator{Store:store,Children:children}
}
func reconcileAuthoredFixture(c AuthoredRegionCoordinator,s *authoredMemoryStore)(AuthoredRegionTicket,error){i:=s.record.Intent;return c.Reconcile(context.Background(),auth.Identity{UserID:i.Actor,SessionID:i.Session},i.Tenant,i.Asset,s.record.ID)}

func TestAuthoredCoordinatorRecoversCommittedChildAfterLostResponse(t *testing.T){
 s,child,c:=authoredCoordinatorFixture();lost:=errors.New("lost admission response");child.startErr=lost
 if _,err:=reconcileAuthoredFixture(c,s);!errors.Is(err,lost){t.Fatal(err)}
 child.startErr=nil
 ticket,err:=reconcileAuthoredFixture(c,s)
 if err!=nil||ticket.Phase!=AuthoredRegionRendering||child.starts!=1||child.crops!=0{t.Fatal(ticket,err,child.starts,child.crops)}
}
func TestAuthoredCoordinatorCancellationPrecedesNewChildAndWaitsForWriters(t *testing.T){
 s,child,c:=authoredCoordinatorFixture();child.facts.AllWritersClosed=false
 i:=s.record.Intent
 ticket,err:=c.Cancel(context.Background(),auth.Identity{UserID:i.Actor,SessionID:i.Session},i.Tenant,i.Asset,s.record.ID)
 if err!=nil||ticket.Phase!=AuthoredRegionCancelling||!s.cancellation||child.starts!=0||child.crops!=0||child.cancels!=1{t.Fatal(ticket,err,child)}
 child.facts.AllWritersClosed=true
 ticket,err=reconcileAuthoredFixture(c,s)
 if err!=nil||ticket.Phase!=AuthoredRegionCancelled{t.Fatal(ticket,err)}
}
func TestAuthoredCoordinatorTerminalStatusDoesNotSubstituteCanonicalProof(t *testing.T){
 s,child,c:=authoredCoordinatorFixture();child.facts.RenderExists=true;child.facts.RenderTerminal=true
 ticket,err:=reconcileAuthoredFixture(c,s)
 if err!=nil||ticket.Phase!=AuthoredRegionFailed||child.crops!=0{t.Fatal(ticket,err)}
}
func TestAuthoredCoordinatorCropStartsOnlyAfterCanonicalRender(t *testing.T){
 s,child,c:=authoredCoordinatorFixture();child.facts.RenderExists=true;child.facts.RenderTerminal=true;child.facts.RenderCanonical=true
 ticket,err:=reconcileAuthoredFixture(c,s)
 if err!=nil||ticket.Phase!=AuthoredRegionCropping||child.crops!=1||child.starts!=0{t.Fatal(ticket,err)}
}
func TestAuthoredCoordinatorDoesNotReturnReadyAfterFinalAuthorityChanges(t *testing.T){
 for _,kind:=range []string{"draft","cancel","identity"}{t.Run(kind,func(t *testing.T){
  s,child,c:=authoredCoordinatorFixture();child.facts.RenderExists=true;child.facts.RenderTerminal=true;child.facts.RenderCanonical=true
  child.facts.CropExists=true;child.facts.CropTerminal=true;child.facts.CropCanonical=true
  child.inspectAt=func(n int){if n==2{switch kind{case "draft":s.record.Current=false;case "cancel":s.record.CancelRequested=true;s.record.Revision++;case "identity":s.record.CropRun=uuid.New()}}}
  ticket,err:=reconcileAuthoredFixture(c,s)
  if kind=="identity"{if !errors.Is(err,v2input.ErrConflict){t.Fatal(ticket,err)};return}
  if err!=nil||ticket.Phase==AuthoredRegionReady{t.Fatal(ticket,err)}
  if kind=="draft"&&ticket.Phase!=AuthoredRegionStale||kind=="cancel"&&ticket.Phase!=AuthoredRegionCancelled{t.Fatal(ticket)}
 })}
}
