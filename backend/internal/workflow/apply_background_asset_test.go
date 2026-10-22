package workflow

import (
 "encoding/json"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/review"
)

func TestApplyPortableBackgroundPinAndDecodedColorAuthority(t *testing.T) {
 style:=review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("a",64)}
 pin:=&review.ApplyBackgroundAsset{PortableID:"backdrop",StyleVersionID:uuid.New(),Style:style,Source:review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("b",64)},ExpectedBytes:512,Suffix:".png"}
 if err:=validateApplyBackgroundAsset(pin,"backdrop",style);err!=nil { t.Fatal(err) }
 foreign:=style;foreign.ID=uuid.New()
 if validateApplyBackgroundAsset(pin,"backdrop",foreign)==nil { t.Fatal("foreign style accepted") }
 if validateApplyBackgroundAsset(pin,"other",style)==nil { t.Fatal("foreign portable ID accepted") }
 target:=review.WorkingImage{Color:review.ColorSpace{ProfileID:"work_rec2020_gamma24_v1",ICCSHA256:strings.Repeat("c",64),Encoding:"gamma24"}}
 decoded:=review.WorkingImage{DType:"float32",Color:target.Color,Canvas:review.Canvas{SourceSHA256:pin.Source.SHA256}}
 if err:=validateApplyDecodedBackground(pin,decoded,target);err!=nil { t.Fatal(err) }
 decoded.Color.ICCSHA256=strings.Repeat("d",64)
 if validateApplyDecodedBackground(pin,decoded,target)==nil { t.Fatal("foreign ICC accepted") }
 decoded.Color=target.Color;decoded.Alpha=&review.ArtifactRef{ID:uuid.New()}
 if validateApplyDecodedBackground(pin,decoded,target)==nil { t.Fatal("alpha silently flattened") }
}

func TestApplyPortableBackgroundMetadataAndDecodeBudget(t *testing.T) {
 pin:=&review.ApplyBackgroundAsset{Source:review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("a",64)},ExpectedBytes:1024}
 metadata:=IngestMetadata{Source:pin.Source,Width:4000,Height:3000,Format:"jpeg"}
 raw,_:=json.Marshal(metadata)
 parsed,err:=applyBackgroundAssetMetadata(raw,pin)
 if err!=nil { t.Fatal(err) }
 profile,err:=applyBackgroundAssetProfile(pin,&parsed,512)
 if err!=nil { t.Fatal(err) }
 estimate,err:=ParseResourceEstimate(profile)
 if err!=nil || estimate.PixelCount!=12000000 || estimate.OutputBytes<144000000 || estimate.ScratchBytes<estimate.OutputBytes+pin.ExpectedBytes { t.Fatalf("invalid decode budget: %+v %v",estimate,err) }
 metadata.HasAlpha=true;raw,_=json.Marshal(metadata)
 if _,err=applyBackgroundAssetMetadata(raw,pin);err==nil { t.Fatal("alpha background admitted") }
 metadata.HasAlpha=false;metadata.Source.ID=uuid.New();raw,_=json.Marshal(metadata)
 if _,err=applyBackgroundAssetMetadata(raw,pin);err==nil { t.Fatal("foreign inspection accepted") }
}

func TestApplyPortableBackgroundDecodePrecedesCompose(t *testing.T) {
 plan,err:=CompileApplyV4Plan(json.RawMessage(`{"mode":"fixed","extensions":{"background":{"mode":"asset"}}}`),2)
 if err!=nil { t.Fatal(err) }
 phases:=[]string{}
 for _, stage:=range plan { phases=append(phases,stage.Phase) }
 if !strings.Contains(strings.Join(phases,","),"background_inspect,background_decode,background") { t.Fatalf("asset decode absent from plan: %v",phases) }
}
