package interchange
import (
 "context"
 "encoding/json"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2portrait"
)
func TestLocalImportParamsPreserveControlsAndRemapBothMasks(t *testing.T){
 for _,kind:=range []string{"portrait.hair","portrait.clothing"}{t.Run(kind,func(t *testing.T){
  for _,change:=range []string{"none","canvas","foreign","unmapped","qualification","cancelled"}{t.Run(change,func(t *testing.T){
   ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(uuid.NewString()))}}
   old:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:ref(),CanvasHash:rawSHA([]byte("canvas"))};target:=old;target.AssetVersionID=uuid.New();target.Source=ref()
   mask,guard:=ref(),ref();var raw []byte;var err error
   if kind=="portrait.hair"{raw,_,err=(v2portrait.HairParams{SchemaVersion:1,Base:old,Mask:mask,Protected:guard,Strength:.43,HueDeltaDegrees:-12,SaturationFactor:1.2,Detail:.64}).Compile()}else{raw,_,err=(v2portrait.ClothingParams{SchemaVersion:1,Base:old,Mask:mask,Protected:guard,Strength:.43,HueDeltaDegrees:-120,SaturationFactor:1.7,WrinkleStrength:.64}).Compile()};if err!=nil{t.Fatal(err)}
   if change=="canvas"{target.CanvasHash=rawSHA([]byte("other"))};if change=="foreign"{old.Source=ref()}
   ops:=&preparedLayerOperationStub{};failure:=errors.New("runtime unavailable");if change=="qualification"{ops.failure=failure}
   calls:=0;resolve:=func(r v2input.Ref,contract string)(v2input.Ref,error){calls++;if contract!="MaskRef"{t.Fatal(contract)};if change=="unmapped"{return r,nil};r.ArtifactID=uuid.NewSHA1(target.AssetVersionID,[]byte(r.ArtifactID.String()));return r,nil}
   ctx,cancel:=context.WithCancel(context.Background());defer cancel();if change=="cancelled"{cancel()}
   got,err:=(ImportLocalPortraitParamsCodec{Qualifier:ops}).RemapImportDocument(ctx,"Params",raw,old,target,resolve)
   if change!="none"{if err==nil||got!=nil{t.Fatal("invalid remap accepted")};if change=="qualification"&&!errors.Is(err,failure){t.Fatal(err)};return}
   if err!=nil||calls!=2||ops.calls!=1||ops.versions[0]!=kind+":1"{t.Fatal("remap/qualification",err)}
   var before,after map[string]json.RawMessage;json.Unmarshal(raw,&before);json.Unmarshal(got,&after)
   for key,value:=range before{if key!="base"&&key!="mask"&&key!="protected"&&string(value)!=string(after[key]){t.Fatal("control changed",key)}}
   _,base,newMask,newGuard,err:=localImportParams(got);if err!=nil||base!=target||newMask.ArtifactID==mask.ArtifactID||newGuard.ArtifactID==guard.ArtifactID{t.Fatal("source/mask remap",err)}
  })}
 })}
}

func TestLocalParamsOwnAuthoredBytesBeforeMaskCallbacks(t *testing.T){
 ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(uuid.NewString()))}}
 old:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:ref(),CanvasHash:rawSHA([]byte("canvas"))};target:=old;target.AssetVersionID=uuid.New();target.Source=ref()
 raw,_,err:=(v2portrait.HairParams{SchemaVersion:1,Base:old,Mask:ref(),Protected:ref(),Strength:.43,SaturationFactor:1,Detail:.64}).Compile();if err!=nil{t.Fatal(err)}
 calls:=0
 resolve:=func(r v2input.Ref,contract string)(v2input.Ref,error){calls++;for i:=range raw{raw[i]='x'};r.ArtifactID=uuid.NewSHA1(target.AssetVersionID,[]byte(r.ArtifactID.String()));return r,nil}
 got,err:=(ImportLocalPortraitParamsCodec{Qualifier:&preparedLayerOperationStub{}}).RemapImportDocument(context.Background(),"Params",raw,old,target,resolve)
 if err!=nil||calls!=2{t.Fatal("caller mutation affected owned Params",err)}
 var typed v2portrait.HairParams;if err:=json.Unmarshal(got,&typed);err!=nil{t.Fatal(err)}
 if typed.Strength!=.43||typed.Detail!=.64||typed.Base!=target{t.Fatal("authored controls/binding changed")}
}
