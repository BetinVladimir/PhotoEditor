package provenance

import (
 "context"
 "encoding/json"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
)

func TestSigningNegativeQualificationMatchesPhysicalParamsAndProfile(t *testing.T){
 pinRef:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}}
 params:=v2optical.NegativeParams{SchemaVersion:1,Base:v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:pinRef(),CanvasHash:strings.Repeat("b",64)},Strength:1,FilmBaseRGB:[3]float64{.9,.8,.7},Calibration:pinRef()}
 raw,_,err:=params.Compile();if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"valid","calibration","runtime","backend","renderer","missing","unknown_pin","extra_null","duplicate","bad_params"}{
  t.Run(mode,func(t *testing.T){
   pin:=v2optical.NegativeQualificationPin{PolicyID:uuid.New(),QualificationID:uuid.New(),IssuerID:uuid.New(),PolicySHA:strings.Repeat("c",64),Report:pinRef(),Calibration:params.Calibration,ModelLockSHA:strings.Repeat("d",64),RuntimeLockSHA:strings.Repeat("e",64),DeviceProfileSHA:strings.Repeat("f",64),DatasetSHA:strings.Repeat("a",64),Backend:"cpu"}
   profile:=map[string]any{"backend":"cpu","renderer_version":"v2.2","negative_runtime_sha":pin.RuntimeLockSHA,"negative_qualification":pin}
   candidate:=append([]byte(nil),raw...)
   switch mode{
   case "calibration":pin.Calibration=pinRef();profile["negative_qualification"]=pin
   case "runtime":profile["negative_runtime_sha"]=strings.Repeat("f",64)
   case "backend":profile["backend"]="mps"
   case "renderer":profile["renderer_version"]="v2.3"
   case "missing":delete(profile,"negative_qualification")
   case "unknown_pin","extra_null":
    body,_:=canonical.Marshal(pin);var fields map[string]any;json.Unmarshal(body,&fields)
    fields["unknown"]=true;if mode=="extra_null"{fields["unknown"]=nil};profile["negative_qualification"]=fields
   case "bad_params":candidate=[]byte(`{}`)
   }
   body,_:=canonical.Marshal(profile)
   if mode=="duplicate"{body=append(body[:len(body)-1],[]byte(`,"backend":"cpu"}`)...)}
   got,err:=DecodeSigningNegativeQualification(body,candidate)
   if mode=="valid"{if err!=nil||got!=pin{t.Fatal("valid negative qualification refused",err)};return}
   if err==nil||got!=(v2optical.NegativeQualificationPin{}){t.Fatal("unbound historical identity accepted",mode)}
  })
 }
}

func TestSigningNegativeReadersRefuseMissingDependencies(t *testing.T){
 if _,err:=(SQLSigningNegativeAdoptionReader{}).ReadSigningAdoptedNegativeRoot(context.Background(),auth.Identity{},ReadySigningExport{},CanonicalSigningTask{});err==nil{t.Fatal("missing adoption dependencies accepted")}
 if _,err:=(SQLSigningNegativeQualificationReader{}).ReadSigningNegativeQualification(context.Background(),auth.Identity{},ReadySigningExport{},CanonicalSigningTask{});err==nil{t.Fatal("missing qualification dependencies accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=(SQLSigningNegativeQualificationReader{}).ReadSigningNegativeQualification(ctx,auth.Identity{},ReadySigningExport{},CanonicalSigningTask{});err!=context.Canceled{t.Fatal("cancellation lost",err)}
}
