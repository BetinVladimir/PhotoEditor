package v2learning
import (
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)
func TestTrainingBackgroundPinStructuralAndRecipeBindings(t *testing.T){
 style:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}
 icc:=strings.Repeat("c",64)
 pin:=v2draft.FrozenBackgroundAsset{SchemaVersion:1,PortableAssetID:"installed",StyleVersionID:uuid.New(),Style:style,Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("b",64)},ExpectedBytes:10,MediaType:"image/png",Suffix:".png",WorkICCSHA:icc}
 if !validTrainingBackgroundPin(nil,icc,style)||!validTrainingBackgroundPin(&pin,icc,style){t.Fatal("valid pin refused")}
 for _,mode:=range []string{"style","icc","source","suffix","bytes"}{t.Run(mode,func(t *testing.T){
  altered:=pin
  switch mode{case "style":altered.Style.ArtifactID=uuid.New();case "icc":altered.WorkICCSHA=strings.Repeat("d",64);case "source":altered.Source.ArtifactID=uuid.Nil;case "suffix":altered.Suffix=".jpg";case "bytes":altered.ExpectedBytes=0}
  if validTrainingBackgroundPin(&altered,icc,style){t.Fatal("invalid pin admitted")}
 })}
}

func TestTrainingBackgroundDecoderUsesFullUnfrozenCanvasBound(t *testing.T){
 if ram,scratch,err:=trainingBackgroundDecodeResources(nil);err!=nil||ram!=0||scratch!=0{t.Fatal("absent asset consumed resources")}
 pin:=v2draft.FrozenBackgroundAsset{SchemaVersion:1,PortableAssetID:"installed",StyleVersionID:uuid.New(),Style:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)},Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("b",64)},ExpectedBytes:536870912,MediaType:"image/png",Suffix:".png",WorkICCSHA:strings.Repeat("c",64)}
 ram,scratch,err:=trainingBackgroundDecodeResources(&pin)
 if err!=nil||ram!=12*200_000_000+536870912+1024*1024*1024+128*512*512||scratch!=536870912+12*200_000_000+1048576{t.Fatalf("decoder resources ram=%d scratch=%d err=%v",ram,scratch,err)}
 pin.ExpectedBytes++
 if _,_,err:=trainingBackgroundDecodeResources(&pin);err==nil{t.Fatal("oversize source admitted")}
}
