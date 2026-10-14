package interchange

import (
 "context"
 "bytes"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "testing"
 "photoeditor/backend/internal/v2input"
)
func TestBodyFitCausalRejectsIncompleteAuthority(t *testing.T){
 // Missing dependencies must refuse before reading any supplied evidence.
 q:=BoundExportBodyFitCausal{}
 if err:=q.QualifyExportBodyFitCausalInputs(context.Background(),v2input.Entry{},nil,ExportClosureNode{},nil);err==nil{t.Fatal("incomplete authority accepted")}
}
func TestBodyFitCausalPreservesCancellation(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 err:=(BoundExportBodyFitCausal{}).QualifyExportBodyFitCausalInputs(ctx,v2input.Entry{},nil,ExportClosureNode{},nil)
 if err!=context.Canceled{t.Fatal("cancellation lost",err)}
}

type causalAlignmentCallback func(context.Context,v2input.Entry,[]byte,ExportClosureNode,ExportClosureNode)error
func(f causalAlignmentCallback)QualifyExportBodyFitAlignment(c context.Context,p v2input.Entry,r []byte,n,ch ExportClosureNode)error{return f(c,p,r,n,ch)}
type causalFeatureCallback func(context.Context,v2input.Entry,[]byte,ExportClosureNode,ExportClosureNode)error
func(f causalFeatureCallback)QualifyExportBodyFitFeature(c context.Context,p v2input.Entry,r []byte,n,ch ExportClosureNode)error{return f(c,p,r,n,ch)}
type causalPersonCallback func(context.Context,v2input.Entry,[]byte,ExportClosureNode)error
func(f causalPersonCallback)QualifyExportBodyFitPairedPersons(c context.Context,p v2input.Entry,r []byte,n ExportClosureNode)error{return f(c,p,r,n)}

// These mocks exercise orchestration and ownership, not SQL/native qualification.
func TestBodyFitCausalFullSequence(t *testing.T){
 for _,mode:=range []string{"valid","faces","alignment_denied","feature_denied","person_denied","changed_before","changed_after_alignment","changed_after_person","missing_feature","cancel_in_alignment"}{t.Run(mode,func(t *testing.T){
  parent,raw,image,mask:=bodyPersonFixture(t)
  originalRaw:=append([]byte(nil),raw...)
  nodes:=&exportNodeResolverStub{nodes:map[uuid.UUID]ExportClosureNode{}}
  add:=func(contract string,body []byte)v2input.Ref{
   ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(body)}
   nodes.nodes[ref.ArtifactID]=ExportClosureNode{Entry:v2input.Entry{Contract:contract,Ref:ref},Body:append([]byte(nil),body...)};return ref
  }
  before:=add("WorkingImage",image.Body);after:=add("WorkingImage",image.Body)
  alignment:=add("PairAlignment",[]byte(`{}`));bp:=add("PoseSet",[]byte(`{}`));ap:=add("PoseSet",[]byte(`{}`))
  params:=map[string]any{"pair_id":uuid.New(),"before":before,"after":after,"alignment":alignment,"before_pose":bp,"after_pose":ap,"before_person":mask,"after_person":mask}
  if mode=="faces"{params["before_faces"]=add("FaceLandmarks",[]byte(`{}`));params["after_faces"]=add("FaceLandmarks",[]byte(`{}`))}
  fitRaw,err:=canonical.Marshal(map[string]any{"params":params,"objects":[]any{}});if err!=nil{t.Fatal(err)}
  fit:=ExportClosureNode{Entry:v2input.Entry{Contract:"BodyFit",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(fitRaw)}},Body:fitRaw}
  reader:=&bodyFitBundleStub{change:mode=="changed_after_alignment"}
  events:=[]string{}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  mutate:=func(r []byte,n ExportClosureNode){if !bytes.Equal(r,originalRaw)||!bytes.Equal(n.Body,fitRaw){t.Fatal("callback received mutated evidence")};r[0]='!';n.Body[0]='!'}
  q:=BoundExportBodyFitCausal{Reader:reader,Documents:nodes}
  q.Alignment=causalAlignmentCallback(func(_ context.Context,_ v2input.Entry,r []byte,n,ch ExportClosureNode)error{
   events=append(events,"alignment");mutate(r,n)
   if ch.Entry.Ref!=alignment{t.Fatal("foreign alignment")};ch.Body[0]='!'
   if mode=="cancel_in_alignment"{cancel()}
   if mode=="alignment_denied"{return v2input.ErrInvalid};return nil
  })
  q.Features=causalFeatureCallback(func(_ context.Context,_ v2input.Entry,r []byte,n,ch ExportClosureNode)error{
   events=append(events,"feature");mutate(r,n)
   if ch.Entry.Contract!="PoseSet"&&ch.Entry.Contract!="FaceLandmarks"{t.Fatal("foreign feature")};ch.Body[0]='!'
   if mode=="feature_denied"{return v2input.ErrInvalid};return nil
  })
  q.Persons=causalPersonCallback(func(_ context.Context,_ v2input.Entry,r []byte,n ExportClosureNode)error{
   events=append(events,"persons");mutate(r,n)
   if mode=="changed_after_person"{reader.change=true}
   if mode=="person_denied"{return v2input.ErrInvalid};return nil
  })
  bundle:=[]byte(`{}`);if mode=="changed_before"{bundle=[]byte(`{"other":true}`)}
  if mode=="missing_feature"{delete(nodes.nodes,bp.ArtifactID)}
  err=q.QualifyExportBodyFitCausalInputs(ctx,parent,raw,fit,bundle)
  valid:=mode=="valid"||mode=="faces"
  if (err==nil)!=valid{t.Fatal("unexpected causal decision",err)}
  if valid{
   count:=4;if mode=="faces"{count=6}
   if len(events)!=count||events[0]!="alignment"||events[len(events)-1]!="persons"||reader.calls!=count+1{t.Fatal("incomplete causal sequence",events,reader.calls)}
  }
  if mode=="changed_before"&&len(events)!=0{t.Fatal("changed bundle reached producer")}
  if !bytes.Equal(raw,originalRaw)||!bytes.Equal(fit.Body,fitRaw){t.Fatal("caller evidence mutated")}
 })}
}
