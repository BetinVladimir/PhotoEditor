package interchange

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)
type exportCorrectionMemberStub struct{calls int;deny bool;binding v2input.SessionVersionBinding}
func(s *exportCorrectionMemberStub)QualifyExportCorrectionMember(ctx context.Context,binding v2input.SessionVersionBinding,parent v2input.Ref,raw []byte)error{
 s.calls++;s.binding=binding;if err:=ctx.Err();err!=nil{return err};if s.deny{return v2input.ErrInvalid};return nil
}
func TestExportCorrectionExtraWorkingRequiresPlanMembershipAndExactVersion(t *testing.T){
 for _,mode:=range []string{"valid","unrelated","canvas","plan_hash","edge","parent","authority","icc","cancelled"}{t.Run(mode,func(t *testing.T){
  _,entry,raw:=importWorkingValidatorFixture(t)
  image,err:=review.ParseWorkingImage(raw);if err!=nil{t.Fatal(err)}
  canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
  extra:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:entry.Ref,CanvasHash:canvas}
  selected:=extra;selected.AssetVersionID=uuid.New();selected.Source.ArtifactID=uuid.New()
  if mode=="unrelated"{extra.Source.ArtifactID=uuid.New()};if mode=="canvas"{extra.CanvasHash=rawSHA([]byte("foreign canvas"))}
  items:=[]any{}
  for index,binding:=range []v2input.SessionVersionBinding{selected,extra}{items=append(items,map[string]any{"ordinal":[]int{2,7}[index],"binding":binding,"delta":v2draft.CorrectionVector{},"disposition":"no_change","reason":nil})}
  planRaw,err:=canonical.Marshal(map[string]any{"schema_version":1,"snapshot_digest":rawSHA([]byte("snapshot")),"items":items});if err!=nil{t.Fatal(err)}
  plan:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(planRaw)},Contract:"CorrectionPlan"}
  style:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("style"))}
  authored,err:=canonical.Marshal(map[string]any{"schema_version":3,"source":selected,"base_style":style,"session_corrections":plan.Ref,"learning_version_id":nil,"overrides":[]v2input.Ref{},"layer_stack":nil,"renderer_version":"v2.2","parameter_delta":v2draft.CorrectionVector{}});if err!=nil{t.Fatal(err)}
  body,err:=v2draft.BuildFrozenRenderRecipe(authored,v2draft.ResolvedRecipeInputs{Binding:selected,BaseStyle:style,Style:v2draft.StyleBaseline{GainsRGB:[3]float64{1,1,1},Saturation:1},Session:&v2draft.ResolvedCorrection{Ref:plan.Ref},ICCLockSHA:image.Color.ICCSHA256,ModelLockSHA:rawSHA([]byte("models"))},nil);if err!=nil{t.Fatal(err)}
  parent:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(body)},Contract:"FrozenRenderRecipe"}
  parents:=&exportMaskNodeStub{node:ExportClosureNode{Entry:parent,Body:body,Children:[]v2input.Entry{plan}}}
  plans:=&exportMaskNodeStub{node:ExportClosureNode{Entry:plan,Body:planRaw,Children:[]v2input.Entry{entry}}}
  member:=&exportCorrectionMemberStub{deny:mode=="authority"}
  additional:=ExportCorrectionWorkingAuthority{Parents:parents,Plans:plans,Recipes:[]exportRecipeBinding{{ref:parent.Ref,binding:selected}},Authority:member}
  working:=ExportWorkingDocumentValidator{masters:map[uuid.UUID]exportWorkingBinding{},icc:image.Color.ICCSHA256,Additional:additional}
  ctx:=context.Background()
  switch mode{case "plan_hash":plans.node.Body=[]byte(`{}`);case "edge":plans.node.Children=nil;case "parent":parents.node.Entry.Ref.ArtifactID=uuid.New();case "icc":working.icc=rawSHA([]byte("foreign profile"));case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  children,err:=working.ExportDocumentChildren(ctx,entry,raw)
  if (err==nil)!=(mode=="valid"){t.Fatalf("unexpected extra source qualification: %v",err)}
  if mode=="valid"&&(len(children)==0||children[0].Contract!="WorkingPixels"||member.calls!=1||member.binding!=extra){t.Fatal("extra physical/version qualification missing")}
  if len(working.masters)!=0{t.Fatal("dependency expanded frozen selection")}
 })}
}
