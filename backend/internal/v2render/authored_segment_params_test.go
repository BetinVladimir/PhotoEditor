package v2render

import (
 "bytes"
 "crypto/sha256"
 "encoding/hex"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

func authoredSegmentParamsFixture()AuthoredSegmentParams{
 ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}}
 layers:=ref()
 return AuthoredSegmentParams{SchemaVersion:1,TenantID:uuid.New(),ParentRunID:uuid.New(),WorkflowID:uuid.New(),InstructionID:uuid.New(),Ordinal:1,ProgramSHA256:strings.Repeat("b",64),PredecessorSHA256:strings.Repeat("c",64),Recipe:ref(),BaseReceipt:ref(),Source:v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:ref(),CanvasHash:strings.Repeat("d",64)},CurrentLayers:&layers,PhaseReceipts:[]v2input.Ref{}}
}

func TestAuthoredSegmentParamsActualCanonicalBodyAndOwnedPointers(t *testing.T){
 params:=authoredSegmentParamsFixture();original:=*params.CurrentLayers
 compiled,err:=CompileAuthoredSegmentParams(params);if err!=nil{t.Fatal(err)}
 params.CurrentLayers.SHA256=strings.Repeat("e",64)
 if *compiled.Params.CurrentLayers!=original{t.Fatal("compiler retained caller mutable pointer")}
 sum:=sha256.Sum256(compiled.JSON);reference:=v2input.Ref{ArtifactID:uuid.New(),SHA256:hex.EncodeToString(sum[:])}
 if err=ValidateAuthoredSegmentParams(compiled,reference);err!=nil{t.Fatal(err)}
 compiled.JSON=append(compiled.JSON,' ')
 if ValidateAuthoredSegmentParams(compiled,reference)==nil{t.Fatal("mutated compiler bytes accepted")}
}

func TestAuthoredSegmentParamsNoopIsExplicitNullAndNeedsAuthorityLater(t *testing.T){
 params:=authoredSegmentParamsFixture();params.CurrentLayers=nil
 compiled,err:=CompileAuthoredSegmentParams(params);if err!=nil{t.Fatal(err)}
 if !bytes.Contains(compiled.JSON,[]byte(`"current_layers":null`)){t.Fatal("noop distinction absent")}
}

func TestAuthoredSegmentParamsRefusesMalformedCanonicalIdentitiesAndPrefix(t *testing.T){
 variants:=map[string]func(*AuthoredSegmentParams){
  "tenant":func(p *AuthoredSegmentParams){p.TenantID=uuid.Nil},
  "upper digest":func(p *AuthoredSegmentParams){p.ProgramSHA256=strings.Repeat("A",64)},
  "source":func(p *AuthoredSegmentParams){p.Source.Source.ArtifactID=uuid.Nil},
  "missing predecessor phase":func(p *AuthoredSegmentParams){p.Ordinal=2},
  "extra predecessor phase":func(p *AuthoredSegmentParams){p.PhaseReceipts=[]v2input.Ref{p.Recipe}},
  "zero current layers":func(p *AuthoredSegmentParams){p.CurrentLayers.ArtifactID=uuid.Nil},
 }
 for name,change:=range variants{t.Run(name,func(t *testing.T){p:=authoredSegmentParamsFixture();change(&p);if _,err:=CompileAuthoredSegmentParams(p);err==nil{t.Fatal("invalid segment params accepted")}})}
}
