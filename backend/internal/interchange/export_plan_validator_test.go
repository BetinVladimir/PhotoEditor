package interchange

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

func TestExportPlanTypedChildrenAndParamsIdentity(t *testing.T){
 workflow,project,snapshot,raw,frame:=exportPlanFixture(t)
 compiled,err:=CompileExportPlan(context.Background(),workflow,project,"xmp",snapshot,raw,ExportLimits{1<<20,1<<20,10},exportFrameResolverFunc(func(context.Context,v2input.SessionSnapshotItem)(ExportFrame,error){return frame,nil}));if err!=nil{t.Fatal(err)}
 pref,body:=compiled.PlanDocument();var plan importPlanDocument;if err:=decodeImportDocument(body,&plan);err!=nil{t.Fatal(err)}
 validator:=ExportPlanDocumentValidator{Task:compiled.TaskID(),Project:project,SnapshotDigest:plan.Snapshot,Plan:pref}
 children,err:=validator.ExportDocumentChildren(context.Background(),v2input.Entry{Ref:pref,Contract:"InterchangePlan"},body);if err!=nil{t.Fatal(err)}
 roles:=map[string]bool{};for _,child:=range children{roles[child.Contract]=true}
 if len(children)!=3||!roles["EncodedImage"]||!roles["WorkingImage"]||!roles["EditingRecipeV3"]{t.Fatal("wrong typed closure",children)}
 paramsRef,params:=compiled.ParamsDocument()
 children,err=validator.ExportDocumentChildren(context.Background(),v2input.Entry{Ref:paramsRef,Contract:"Params"},params)
 if err!=nil||len(children)!=1||children[0].Ref!=pref{t.Fatal("wrong params closure",children,err)}
 paramsRef.ArtifactID=uuid.New()
 if _,err:=validator.ExportDocumentChildren(context.Background(),v2input.Entry{Ref:paramsRef,Contract:"Params"},params);err==nil{t.Fatal("foreign params identity")}
}

func TestExportPlanRejectsNullableBindingsAndForeignScope(t *testing.T){
 for _,mode:=range []string{"binding","recipe","project","snapshot"}{t.Run(mode,func(t *testing.T){
  workflow,project,snapshot,raw,frame:=exportPlanFixture(t)
  compiled,err:=CompileExportPlan(context.Background(),workflow,project,"xmp",snapshot,raw,ExportLimits{1<<20,1<<20,10},exportFrameResolverFunc(func(context.Context,v2input.SessionSnapshotItem)(ExportFrame,error){return frame,nil}));if err!=nil{t.Fatal(err)}
  pref,body:=compiled.PlanDocument();var plan importPlanDocument;if err:=decodeImportDocument(body,&plan);err!=nil{t.Fatal(err)}
  digest:=plan.Snapshot
  switch mode{case "binding":plan.Entries[0].Binding=nil;case "recipe":plan.Entries[0].Recipe=nil;case "project":project=uuid.New();case "snapshot":digest=rawSHA([]byte("other"))}
  body,err=canonical.Marshal(plan);if err!=nil{t.Fatal(err)};pref.SHA256=rawSHA(body)
  validator:=ExportPlanDocumentValidator{Task:compiled.TaskID(),Project:project,SnapshotDigest:digest,Plan:pref}
  if _,err:=validator.ExportDocumentChildren(context.Background(),v2input.Entry{Ref:pref,Contract:"InterchangePlan"},body);err==nil{t.Fatal("invalid export accepted")}
 })}
}
