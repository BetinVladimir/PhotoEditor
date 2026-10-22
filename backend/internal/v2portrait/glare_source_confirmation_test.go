package v2portrait

import (
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

func sourceGlareFixture(t *testing.T)GlareSourceParams{
 t.Helper();ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}}
 return GlareSourceParams{SchemaVersion:1,Base:v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:ref(),CanvasHash:strings.Repeat("b",64)},Strength:0.5,Mode:"generative",GlassesMask:ref(),Protected:ref(),Confirmation:ref(),Calibration:ref(),SourceFaces:ref(),ModelLockSHA:strings.Repeat("c",64)}
}
func TestParseGlareSourceParamsRequiresExplicitSourceOnlyContract(t *testing.T){
 p:=sourceGlareFixture(t);raw,err:=canonical.Marshal(p);if err!=nil{t.Fatal(err)}
 if _,err=ParseGlareSourceParams(raw);err!=nil{t.Fatal(err)}
 for name,mutate:=range map[string]func(*GlareSourceParams){
  "donor":func(p *GlareSourceParams){b:=p.Base;p.Donor=&b},
  "donor faces":func(p *GlareSourceParams){r:=p.SourceFaces;p.DonorFaces=&r},
  "mode":func(p *GlareSourceParams){p.Mode="donor"},
  "duplicate refs":func(p *GlareSourceParams){p.GlassesMask=p.Protected},
  "invalid source":func(p *GlareSourceParams){p.Base.AssetVersionID=uuid.Nil},
  "invalid qualification":func(p *GlareSourceParams){p.ModelLockSHA="invalid"},
  "strength":func(p *GlareSourceParams){p.Strength=1.01},
 }{t.Run(name,func(t *testing.T){q:=p;mutate(&q);b,e:=canonical.Marshal(q);if e!=nil{t.Fatal(e)};if _,e=ParseGlareSourceParams(b);e==nil{t.Fatal("accepted invalid source-only contract")}})}
 // Exact canonical typed roundtrip rejects unknown fields and absent explicit
 // null donor fields, even though encoding/json would accept them.
 for _,bad:=range []string{strings.Replace(string(raw),"{","{\"unknown\":true,",1),strings.Replace(string(raw),"\"donor\":null,","",1),string(raw)+" "}{if _,err=ParseGlareSourceParams([]byte(bad));err==nil{t.Fatal("accepted non-exact contract")}}
}
