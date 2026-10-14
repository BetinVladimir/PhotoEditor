package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestBodyFitInventoryPreservesWorkerRequiredInputs(t *testing.T){
 for _,mode:=range []string{"valid","faces","unpaired_faces","pose_null","hash","contract"}{t.Run(mode,func(t *testing.T){
  _,_,image,mask:=bodyPersonFixture(t)
  ref:=func(name string)v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(name))}}
  before,after,alignment,bp,ap,bf,af:=image.Entry.Ref,ref("after"),ref("alignment"),ref("before_pose"),ref("after_pose"),ref("before_faces"),ref("after_faces")
  afterMask:=mask;afterMask.Artifact.ID=uuid.New()
  params:=map[string]any{"pair_id":uuid.New(),"before":before,"after":after,"alignment":alignment,"before_pose":bp,"after_pose":ap,"before_person":mask,"after_person":afterMask}
  if mode=="faces"||mode=="unpaired_faces"{params["before_faces"]=bf};if mode=="faces"{params["after_faces"]=af};if mode=="pose_null"{params["after_pose"]=nil}
  raw,err:=canonical.Marshal(map[string]any{"params":params,"objects":[]any{}});if err!=nil{t.Fatal(err)}
  node:=ExportClosureNode{Entry:v2input.Entry{Contract:"BodyFit",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}},Body:raw}
  if mode=="hash"{node.Entry.Ref.SHA256=rawSHA([]byte("foreign"))};if mode=="contract"{node.Entry.Contract="Params"}
  inventory,err:=exportBodyFitInputInventory(node)
  if mode!="valid"&&mode!="faces"{if err==nil||inventory.Inputs!=nil{t.Fatal("invalid fit inventory accepted",err)};return}
  expected:=[]v2input.Ref{before,after,alignment,bp,ap,{ArtifactID:mask.Artifact.ID,SHA256:mask.Artifact.SHA256},{ArtifactID:afterMask.Artifact.ID,SHA256:afterMask.Artifact.SHA256}}
  if mode=="faces"{expected=append(expected,bf,af)}
  if err!=nil||inventory.Before!=before||inventory.After!=after||len(inventory.Inputs)!=len(expected){t.Fatal("paired inventory lost",err)}
  for i:=range expected{if inventory.Inputs[i]!=expected[i]{t.Fatal("worker input order/pin changed",i)}}
 })}
}
func TestBodyFitInventorySQLRequiresDatabaseAndContext(t *testing.T){
 source:=SQLExportDocumentCatalog{}
 if err:=source.QualifyExportBodyFitInputInventory(context.Background(),v2input.Entry{},nil,ExportClosureNode{});err!=v2input.ErrInvalid{t.Fatal(err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=source.QualifyExportBodyFitInputInventory(ctx,v2input.Entry{},nil,ExportClosureNode{});err!=context.Canceled{t.Fatal(err)}
}
