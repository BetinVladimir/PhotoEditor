package interchange

import (
 "context"
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
type bodyFitInventoryStub struct{calls int;reject int}
func(s *bodyFitInventoryStub)QualifyExportBodyFitInputInventory(_ context.Context,_ v2input.Entry,raw []byte,fit ExportClosureNode)error{
 s.calls++;if s.calls==s.reject{return v2input.ErrInvalid}
 // Caller evidence must remain owned despite a mutating authority callback.
 if len(raw)>0{raw[0]='!'};if len(fit.Body)>0{fit.Body[0]='!'};return nil
}
func TestBodyFitReaderPinnedPair(t *testing.T){
 for _,mode:=range []string{"valid","missing","changed","inventory_before","inventory_after","hash","cancelled"}{t.Run(mode,func(t *testing.T){
  parent,raw,image,mask:=bodyPersonFixture(t)
  nodes:=&exportNodeResolverStub{nodes:map[uuid.UUID]ExportClosureNode{}}
  add:=func(contract string,body []byte)v2input.Ref{
   ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(body)}
   nodes.nodes[ref.ArtifactID]=ExportClosureNode{Entry:v2input.Entry{Ref:ref,Contract:contract},Body:append([]byte(nil),body...)};return ref
  }
  before:=add("WorkingImage",image.Body);after:=add("WorkingImage",image.Body)
  alignment:=add("PairAlignment",[]byte(`{}`));bp:=add("PoseSet",[]byte(`{}`));ap:=add("PoseSet",[]byte(`{}`))
  params:=map[string]any{"pair_id":uuid.New(),"before":before,"after":after,"alignment":alignment,"before_pose":bp,"after_pose":ap,"before_person":mask,"after_person":mask}
  fitRaw,err:=canonical.Marshal(map[string]any{"params":params,"objects":[]any{}});if err!=nil{t.Fatal(err)}
  fit:=ExportClosureNode{Entry:v2input.Entry{Contract:"BodyFit",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(fitRaw)}},Body:fitRaw}
  authority:=&bodyFitInventoryStub{}
  if mode=="inventory_before"{authority.reject=1};if mode=="inventory_after"{authority.reject=2}
  if mode=="missing"{delete(nodes.nodes,ap.ArtifactID)}
  if mode=="hash"{fit.Entry.Ref.SHA256=rawSHA([]byte("other"))}
  reader:=ExportBodyFitReader{Inventory:authority,Documents:bodyOriginChangingDocuments{base:nodes,change:mode=="changed"}}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  bundle,err:=reader.ReadExportBodyFitInputs(ctx,parent,raw,fit)
  if mode!="valid"{if err==nil||bundle!=nil{t.Fatal("invalid pair returned",err)};return}
  var decoded exportBodyFitBundle
  if err!=nil||json.Unmarshal(bundle,&decoded)!=nil||authority.calls!=2||nodes.calls!=10||decoded.Before.SHA!=before.SHA256||decoded.After.SHA!=after.SHA256||decoded.Alignment.SHA!=alignment.SHA256||decoded.BeforeFaces!=nil||decoded.AfterFaces!=nil{t.Fatal("pair bundle incomplete",err)}
 })}
}
