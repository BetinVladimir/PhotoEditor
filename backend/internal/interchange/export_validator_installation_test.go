package interchange

import (
 "context"
 "testing"
 "photoeditor/backend/internal/v2input"
)

func TestExportValidatorInstallationBuildsOwnedScopedChain(t *testing.T){
 _,_,intent,frozen,_:=exportExecutionFixture(t)
 semantics:=&exportSemanticsStub{}
 other:=exportReaderValidator{&importValidatorStub{}}
 installed:=ExportDocumentValidatorInstallation{ICCSHA:rawSHA([]byte("icc")),ModelSHA:rawSHA([]byte("model")),Semantics:semantics,Other:other}
 validator,err:=installed.BuildExportDocumentValidator(context.Background(),intent,frozen);if err!=nil{t.Fatal(err)}
 working,ok:=validator.(ExportWorkingDocumentValidator);if !ok||working.icc!=installed.ICCSHA||len(working.masters)!=1{t.Fatal("missing working/source pins")}
 render,ok:=working.Dependencies.(ExportFrozenDocumentValidator);if !ok||render.icc!=installed.ICCSHA||render.model!=installed.ModelSHA||len(render.recipes)!=1{t.Fatal("missing frozen model/source pins")}
 portable,ok:=render.Dependencies.(ExportRecipeDocumentValidator);if !ok||len(portable.recipes)!=1{t.Fatal("missing portable source pins")}
 dependency,ok:=portable.Dependencies.(ExportDependencyDocumentValidator);if !ok||dependency.Other==nil{t.Fatal("missing mandatory semantics")}
 masks,ok:=dependency.Semantics.(*ExportCanonicalMaskSemantics);if !ok||masks.fallback!=semantics{t.Fatal("missing canonical mask/source semantics")}
 installed.ICCSHA=rawSHA([]byte("caller changed"));if working.icc==installed.ICCSHA{t.Fatal("caller changed installed lock")}
 // Synthetic graph fixture's {} master must still fail actual typed validation.
 for _,master:=range working.masters{
  if _,err:=validator.ExportDocumentChildren(context.Background(),v2input.Entry{Ref:master.ref,Contract:"WorkingImage"},[]byte("{}"));err==nil{t.Fatal("graph fixture mistaken for qualified master")}
 }
}

func TestExportValidatorInstallationRejectsMissingPolicies(t *testing.T){
 _,_,intent,frozen,_:=exportExecutionFixture(t)
 for _,mode:=range []string{"semantics","other","icc","model","cancelled"}{t.Run(mode,func(t *testing.T){
  installation:=ExportDocumentValidatorInstallation{ICCSHA:rawSHA([]byte("icc")),ModelSHA:rawSHA([]byte("model")),Semantics:&exportSemanticsStub{},Other:exportReaderValidator{&importValidatorStub{}}}
  ctx:=context.Background()
  switch mode{case "semantics":installation.Semantics=nil;case "other":installation.Other=nil;case "icc":installation.ICCSHA="invalid";case "model":installation.ModelSHA="invalid";case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  if _,err:=installation.BuildExportDocumentValidator(ctx,intent,frozen);err==nil{t.Fatal("incomplete policies installed")}
 })}
}
