package workflow

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

func TestWatermarkChildResolverRefusesMissingDependenciesAndCancellation(t *testing.T){
 var resolver SQLDeliveryWatermarkChildResolver
 result,err:=resolver.ResolveWatermarkChild(context.Background(),delivery.ExpansionLease{},delivery.PreparePlan{},delivery.ExportChildIntent{})
 if err==nil||len(result.ImageJCS)!=0||len(result.Entries)!=0{t.Fatal("empty resolver accepted",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=resolver.ResolveWatermarkChild(ctx,delivery.ExpansionLease{},delivery.PreparePlan{},delivery.ExportChildIntent{});err!=context.Canceled{t.Fatal("cancel ignored",err)}
}

func TestWatermarkSourceDescriptorsRequireExactAuthorizedInventory(t *testing.T){
 _,_,_,raw,_:=deliveryExportPlanFixture(t,nil)
 image,err:=review.ParseWorkingImage(raw);if err!=nil{t.Fatal(err)}
 refs:=[]v2input.Ref{{ArtifactID:image.Pixels.ID,SHA256:image.Pixels.SHA256}}
 if image.Alpha!=nil{refs=append(refs,v2input.Ref{ArtifactID:image.Alpha.ID,SHA256:image.Alpha.SHA256})}
 for _,scenario:=range []string{"valid","missing","sha","contract"}{t.Run(scenario,func(t *testing.T){
  metadata:=map[uuid.UUID]deliverySourceEntry{}
  for _,ref:=range refs{metadata[ref.ArtifactID]=deliverySourceEntry{Ref:ref,Contract:"NpyArray",Key:"exact-key",Version:"exact-version",Bytes:128,MediaType:"application/x-npy"}}
  first:=metadata[refs[0].ArtifactID]
  switch scenario{
  case "missing":delete(metadata,refs[0].ArtifactID)
  case "sha":first.Ref.SHA256="changed";metadata[refs[0].ArtifactID]=first
  case "contract":first.Contract="WorkingImage";metadata[refs[0].ArtifactID]=first
  }
  result,err:=watermarkSourceDescriptors(raw,metadata)
  if scenario!="valid"{if err==nil||result!=nil{t.Fatal("unauthorized array accepted",scenario)};return}
  if err!=nil||len(result)!=len(refs){t.Fatal(result,err)}
  for n,array:=range result{
   contract:="WorkingPixels";if n==1{contract="WorkingAlpha"}
   if array.Ref!=refs[n]||array.Contract!=contract||array.Object.Key!="exact-key"||array.Object.VersionID!="exact-version"||array.Object.SHA256!=refs[n].SHA256{t.Fatal("descriptor changed",array)}
  }
 })}
}
