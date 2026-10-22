package v2region

import (
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

func TestRegionExecutionCropClosureUsesTypedPlanNotArbitraryArtifact(t *testing.T){
 ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:regionSHA}}
 parent:=v2input.Entry{Ref:ref(),Contract:"Params"};image,plan:=ref(),ref()
 raw,err:=canonical.Marshal(map[string]any{"image":image,"plan":plan,"interpolation":"lanczos4"});if err!=nil{t.Fatal(err)}
 children,err:=regionExecutionChildren(raw,parent);if err!=nil||len(children)!=2{t.Fatal(children,err)}
 found:=map[string]v2input.Ref{};for _,child:=range children{found[child.Contract]=child.Ref}
 if found["WorkingImage"]!=image||found["CompositionPlan"]!=plan{t.Fatal("crop dependencies lost",found)}
}

func TestRegionExecutionClosureRejectsUnknownReferenceOccurrence(t *testing.T){
 ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:regionSHA}
 raw,err:=canonical.Marshal(map[string]any{"image":ref,"extra_authority":ref});if err!=nil{t.Fatal(err)}
 if _,err=regionExecutionChildren(raw,v2input.Entry{Ref:ref,Contract:"Params"});err==nil{t.Fatal("unknown reference granted authority")}
}

func TestRegionExecutionBodyReportPreservesHistoricalTypedDependencies(t *testing.T){
 ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:regionSHA}}
 image,poses,person,faces:=ref(),ref(),ref(),ref()
 raw,err:=canonical.Marshal(map[string]any{"image":image,"poses":poses,"person":person,"faces":faces});if err!=nil{t.Fatal(err)}
 children,err:=regionExecutionChildren(raw,v2input.Entry{Ref:ref(),Contract:"BodyWarpReport"});if err!=nil||len(children)!=4{t.Fatal(children,err)}
 found:=map[string]v2input.Ref{};for _,child:=range children{found[child.Contract]=child.Ref}
 if found["WorkingImage"]!=image||found["PoseSet"]!=poses||found["NpyArray"]!=person||found["FaceLandmarks"]!=faces{t.Fatal("body dependencies substituted",found)}
}
