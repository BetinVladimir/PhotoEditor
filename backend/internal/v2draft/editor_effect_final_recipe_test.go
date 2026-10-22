package v2draft

import (
 "encoding/json"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

func TestEditorFinalFreezeRequiresReceiptAndNeverRepeatsAuthoredEffects(t *testing.T){
 authored,inputs,style,overrides,source:=editorEffectsFixture(t,map[string]any{"strength":.4},CorrectionVector{EV:.1})
 original,err:=BuildFrozenRenderRecipeFromDocuments(authored,inputs,nil,style,overrides,source);if err!=nil{t.Fatal(err)}
 pin:=func(raw []byte)v2input.Ref{sha,_:=canonical.Hash(json.RawMessage(raw));return v2input.Ref{ArtifactID:uuid.New(),SHA256:sha}}
 originalRef:=pin(original);build:=uuid.New();processed:=inputs.Binding;processed.Source=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("f",64)};processed.CanvasHash=strings.Repeat("e",64)
 receipt:=EditorExecutedBaseReceipt{SchemaVersion:1,BuildRunID:build,OriginalRecipe:originalRef,OriginalSource:inputs.Binding,ProcessedSource:processed,ExecutedStyle:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)},CompilationSHA:strings.Repeat("b",64),TraceSHA:strings.Repeat("c",64),Quality:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("d",64)},ImageResult:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("e",64)},InheritedReview:true}
 receiptRaw,_:=canonical.Marshal(receipt);receiptRef:=pin(receiptRaw)
 output,err:=BuildEditorFinalRecipe(original,originalRef,receiptRef,receiptRaw,build);if err!=nil{t.Fatal(err)}
 frozen,err:=DecodeFrozenRenderRecipe(output);if err!=nil{t.Fatal(err)}
 if frozen.Scalars.Source!=processed||frozen.Scalars.BaseStyle!=receipt.ExecutedStyle||frozen.Scalars.EffectiveDelta!=(CorrectionVector{})||frozen.Learning!=nil||frozen.BackgroundAsset!=nil{t.Fatal("executed effects would repeat")}
 if v2input.ValidateEditingRecipeV3(frozen.Recipe,processed)==nil{t.Fatal("public draft path accepts private receipt")}
 if v2input.ValidateExecutedEditingRecipeV3(frozen.Recipe,processed)!=nil{t.Fatal("private final shape invalid")}
 deps,err:=FrozenRenderRecipeDependencies(output,pin(output));if err!=nil{t.Fatal(err)}
 receiptFound:=false;for _,dep:=range deps{if dep.Contract=="EditorExecutedBaseReceipt"&&dep.Ref==receiptRef{receiptFound=true}}
 if !receiptFound{t.Fatal("final manifest omits executed receipt")}
 receiptDeps,err:=DocumentChildren(receiptRaw,"EditorExecutedBaseReceipt",receiptRef);if err!=nil{t.Fatal(err)}
 expected:=map[string]v2input.Ref{"FrozenRenderRecipe":originalRef,"StyleV2":receipt.ExecutedStyle,"QualityReport":receipt.Quality,"ImageResult":receipt.ImageResult}
 for _,dep:=range receiptDeps{if ref,ok:=expected[dep.Contract];ok{if dep.Ref!=ref{t.Fatal("wrong receipt dependency pin")};delete(expected,dep.Contract)}}
 if len(expected)!=0||len(receiptDeps)!=6{t.Fatal("receipt ancestry incomplete")}
 malformed:=receipt;malformed.ProcessedSource.Source.ArtifactID=uuid.Nil;malformedRaw,_:=canonical.Marshal(malformed)
 if _,err=DecodeEditorExecutedBaseReceipt(malformedRaw);err==nil{t.Fatal("empty processed source accepted")}
 var unknown map[string]any;json.Unmarshal(receiptRaw,&unknown);unknown["authority"]=true;unknownRaw,_:=canonical.Marshal(unknown)
 if _,err=DecodeEditorExecutedBaseReceipt(unknownRaw);err==nil{t.Fatal("unknown receipt field accepted")}
 var fields map[string]json.RawMessage;json.Unmarshal(frozen.Recipe,&fields);if string(fields["overrides"])!="[]"||string(fields["layer_stack"])!="null"||fields["executed_base_receipt"]==nil{t.Fatal("final lost seal or retained authored layers")}
 foreign:=receipt;foreign.BuildRunID=uuid.New();foreignRaw,_:=canonical.Marshal(foreign)
 if _,err=BuildEditorFinalRecipe(original,originalRef,pin(foreignRaw),foreignRaw,build);err==nil{t.Fatal("foreign build accepted")}
 if _,err=BuildEditorFinalRecipe(original,originalRef,receiptRef,append(receiptRaw,' '),build);err==nil{t.Fatal("changed receipt bytes accepted")}
}
