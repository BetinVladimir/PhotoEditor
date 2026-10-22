package v2draft

import (
 "encoding/json"
 "os"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
)

func TestEditorEffectFrozenProtectionIsCarriedWithoutChangingStyle(t *testing.T){
 fixture,err:=os.ReadFile("../review/testdata/working-images-python.json");if err!=nil{t.Fatal(err)}
 var images []json.RawMessage;if json.Unmarshal(fixture,&images)!=nil||len(images)==0{t.Fatal("missing fixture")}
 image,err:=review.ParseWorkingImage(images[0]);if err!=nil{t.Fatal(err)}
 canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
 mask:=review.MaskRef{Artifact:review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("a",64)},Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"manual",Confidence:1,Protected:true}
 raw,err:=canonical.Marshal(mask);if err!=nil{t.Fatal(err)}
 authored,inputs,style,bodies,source:=editorEffectsFixture(t,nil,CorrectionVector{})
 compilation,err:=CompileEditorEffects(authored,inputs,style,bodies,source);if err!=nil{t.Fatal(err)}
 // The independent fixture mask is bound to the same encoded source here.
 var fields map[string]json.RawMessage;json.Unmarshal(compilation.Override,&fields);fields["input_sha256"],_=canonical.Marshal(image.Canvas.SourceSHA256);compilation.Override,_=canonical.Marshal(fields)
 result,err:=bindEditorEffectProtection(compilation,raw,canvas,image.Canvas.SourceSHA256);if err!=nil{t.Fatal(err)}
 if string(result.Style)!=string(compilation.Style){t.Fatal("protection changed executed style")}
 var out struct{Masks []struct{ID string `json:"binding_id"`;Purpose string `json:"purpose"`;Mask review.MaskRef `json:"mask"`} `json:"mask_refs"`}
 if json.Unmarshal(result.Override,&out)!=nil||len(out.Masks)!=1||out.Masks[0].Purpose!="protected"||out.Masks[0].Mask.Artifact!=mask.Artifact{t.Fatal("frozen protection lost")}
 if _,err=bindEditorEffectProtection(result,raw,canvas,image.Canvas.SourceSHA256);err==nil{t.Fatal("reserved binding collision accepted")}
 if _,err=bindEditorEffectProtection(compilation,raw,strings.Repeat("f",64),image.Canvas.SourceSHA256);err==nil{t.Fatal("foreign canvas accepted")}
 if _,err=bindEditorEffectProtection(compilation,raw,canvas,strings.Repeat("f",64));err==nil{t.Fatal("foreign source accepted")}
 mask.Protected=false;raw,_=canonical.Marshal(mask);if _,err=bindEditorEffectProtection(compilation,raw,canvas,image.Canvas.SourceSHA256);err==nil{t.Fatal("ordinary mask promoted to protection")}
}
