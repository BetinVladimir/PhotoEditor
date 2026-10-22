package v2render

import (
 "bytes"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 _ "embed"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

func authoredFinalFixture() AuthoredFinalParams {
 ref:=func() v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}}
 return AuthoredFinalParams{SchemaVersion:1,TenantID:uuid.New(),ParentRunID:uuid.New(),WorkflowID:uuid.New(),InstructionID:uuid.New(),ProgramSHA256:strings.Repeat("b",64),Recipe:ref(),BaseReceipt:ref(),GeometryContinuation:ref(),Source:v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:ref(),CanvasHash:strings.Repeat("c",64)},RegisteredOriginal:ref(),PhaseReceipts:[]v2input.Ref{ref(),ref()}}
}
func authoredParamsRef(c CompiledAuthoredFinalParams) v2input.Ref {sum:=sha256.Sum256(c.JSON);return v2input.Ref{ArtifactID:uuid.New(),SHA256:hex.EncodeToString(sum[:])}}

func TestAuthoredFinalParamsClosedWireAndOwnership(t *testing.T) {
 p:=authoredFinalFixture();c,err:=CompileAuthoredFinalParams(p);if err!=nil{t.Fatal(err)}
 original:=c.Params.PhaseReceipts[0];p.PhaseReceipts[0]=p.Recipe
 if c.Params.PhaseReceipts[0]!=original{t.Fatal("caller aliases compiled phase order")}
 entries,err:=AuthoredFinalInputRoots(c,authoredParamsRef(c));if err!=nil||len(entries)!=8{t.Fatalf("roots %v %v",entries,err)}
 contracts:=map[string]int{};for i,e:=range entries {contracts[e.Contract]++;if i>0&&bytes.Compare(entries[i-1].Ref.ArtifactID[:],e.Ref.ArtifactID[:])>=0{t.Fatal("not strictly sorted")}}
 if contracts["AuthoredCanonicalPhaseReceipt"]!=2||contracts["WorkingImage"]!=2||contracts["AuthoredCompoundContinuation"]!=1||contracts["FrozenRenderRecipe"]!=1||contracts["AuthoredBaseReceipt"]!=1||contracts["Params"]!=1{t.Fatalf("wrong closure roots %v",contracts)}
 reversed:=c.Params;reversed.PhaseReceipts=[]v2input.Ref{c.Params.PhaseReceipts[1],c.Params.PhaseReceipts[0]};other,err:=CompileAuthoredFinalParams(reversed);if err!=nil||bytes.Equal(c.JSON,other.JSON){t.Fatal("phase order lost")}
}
func TestAuthoredFinalParamsRejectsMalformedInputs(t *testing.T) {
 cases:=map[string]func(*AuthoredFinalParams){
 "schema":func(p *AuthoredFinalParams){p.SchemaVersion=2},"tenant":func(p *AuthoredFinalParams){p.TenantID=uuid.Nil},"parent":func(p *AuthoredFinalParams){p.ParentRunID=uuid.Nil},"workflow":func(p *AuthoredFinalParams){p.WorkflowID=uuid.Nil},"instruction":func(p *AuthoredFinalParams){p.InstructionID=uuid.Nil},"program":func(p *AuthoredFinalParams){p.ProgramSHA256=strings.Repeat("A",64)},"version":func(p *AuthoredFinalParams){p.Source.AssetVersionID=uuid.Nil},"canvas":func(p *AuthoredFinalParams){p.Source.CanvasHash="bad"},"ref":func(p *AuthoredFinalParams){p.GeometryContinuation.ArtifactID=uuid.Nil},"empty":func(p *AuthoredFinalParams){p.PhaseReceipts=nil},"duplicate":func(p *AuthoredFinalParams){p.PhaseReceipts[1]=p.PhaseReceipts[0]},"base_cycle":func(p *AuthoredFinalParams){p.PhaseReceipts[0]=p.BaseReceipt},"recipe_cycle":func(p *AuthoredFinalParams){p.PhaseReceipts[0]=p.Recipe},"phase_sha":func(p *AuthoredFinalParams){p.PhaseReceipts[0].SHA256="bad"},"oversize":func(p *AuthoredFinalParams){p.PhaseReceipts=make([]v2input.Ref,4999)},
 }
 for name,mutate:=range cases{t.Run(name,func(t *testing.T){p:=authoredFinalFixture();mutate(&p);if _,err:=CompileAuthoredFinalParams(p);err==nil{t.Fatal("accepted invalid input")}})}
}
func TestAuthoredFinalRootsRefuseTamperingAndCrossContractAliases(t *testing.T) {
 p:=authoredFinalFixture();c,_:=CompileAuthoredFinalParams(p);ref:=authoredParamsRef(c)
 altered:=c;altered.JSON=append([]byte(nil),c.JSON...);altered.JSON[0]='['
 if _,err:=AuthoredFinalInputRoots(altered,ref);err==nil{t.Fatal("cached JSON accepted")}
 bad:=ref;bad.SHA256=strings.Repeat("0",64);if _,err:=AuthoredFinalInputRoots(c,bad);err==nil{t.Fatal("wrong Params hash")}
 bad=ref;bad.ArtifactID=p.Recipe.ArtifactID;if _,err:=AuthoredFinalInputRoots(c,bad);err==nil{t.Fatal("Params/recipe ID alias")}
 p.RegisteredOriginal=p.Recipe;c,_=CompileAuthoredFinalParams(p);if _,err:=AuthoredFinalInputRoots(c,authoredParamsRef(c));err==nil{t.Fatal("WorkingImage/recipe alias")}
 p=authoredFinalFixture();p.RegisteredOriginal=p.Source.Source;c,_=CompileAuthoredFinalParams(p);roots,err:=AuthoredFinalInputRoots(c,authoredParamsRef(c));if err!=nil||len(roots)!=7{t.Fatal("exact same-contract source must deduplicate",err)}
}

//go:embed testdata/authored_final_params.json
var authoredFinalPythonWire []byte

func TestAuthoredFinalParamsMatchesPythonCanonicalWire(t *testing.T) {
 var p AuthoredFinalParams
 if err:=json.Unmarshal(authoredFinalPythonWire,&p);err!=nil{t.Fatal(err)}
 c,err:=CompileAuthoredFinalParams(p)
 if err!=nil || !bytes.Equal(c.JSON,authoredFinalPythonWire){t.Fatalf("Python/Go canonical wire drift: %v",err)}
}

func TestAuthoredFinalParamsMaximumPhaseCount(t *testing.T) {
 p:=authoredFinalFixture();p.PhaseReceipts=make([]v2input.Ref,4998)
 for i:=range p.PhaseReceipts {p.PhaseReceipts[i]=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}}
 c,err:=CompileAuthoredFinalParams(p);if err!=nil{t.Fatal(err)}
 roots,err:=AuthoredFinalInputRoots(c,authoredParamsRef(c));if err!=nil||len(roots)!=5004{t.Fatalf("full legal bounded root inventory: %d %v",len(roots),err)}
}

func TestAuthoredFinalRootsRejectEveryCrossContractAlias(t *testing.T) {
 cases:=map[string]func(*AuthoredFinalParams){
 "base_continuation":func(p *AuthoredFinalParams){p.GeometryContinuation=p.BaseReceipt},
 "source_base":func(p *AuthoredFinalParams){p.Source.Source=p.BaseReceipt},
 "source_continuation":func(p *AuthoredFinalParams){p.Source.Source=p.GeometryContinuation},
 "source_phase":func(p *AuthoredFinalParams){p.Source.Source=p.PhaseReceipts[0]},
 "phase_continuation":func(p *AuthoredFinalParams){p.PhaseReceipts[0]=p.GeometryContinuation},
 "phase_base_different_sha":func(p *AuthoredFinalParams){p.PhaseReceipts[0]=p.BaseReceipt;p.PhaseReceipts[0].SHA256=strings.Repeat("e",64)},
 "original_phase":func(p *AuthoredFinalParams){p.RegisteredOriginal=p.PhaseReceipts[0]},
 }
 for name,mutate:=range cases{t.Run(name,func(t *testing.T){p:=authoredFinalFixture();mutate(&p);c,err:=CompileAuthoredFinalParams(p);if err==nil{_,err=AuthoredFinalInputRoots(c,authoredParamsRef(c))};if err==nil{t.Fatal("cross-contract identity accepted")}})}
 p:=authoredFinalFixture();p.PhaseReceipts[1]=p.PhaseReceipts[0];p.PhaseReceipts[1].SHA256=strings.Repeat("e",64)
 if _,err:=CompileAuthoredFinalParams(p);err==nil{t.Fatal("duplicate phase identity with different SHA accepted")}
}

func TestAuthoredFinalRootsRejectParamsAliasAndCompiledSliceMutation(t *testing.T) {
 p:=authoredFinalFixture();c,_:=CompileAuthoredFinalParams(p)
 roots,err:=AuthoredFinalInputRoots(c,authoredParamsRef(c));if err!=nil{t.Fatal(err)}
 for _,root:=range roots {
  if root.Contract=="Params"{continue}
  bad:=authoredParamsRef(c);bad.ArtifactID=root.Ref.ArtifactID
  if _,err:=AuthoredFinalInputRoots(c,bad);err==nil{t.Fatalf("Params aliases %s",root.Contract)}
 }
 pinned:=authoredParamsRef(c);c.Params.PhaseReceipts[0]=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("d",64)}
 if _,err:=AuthoredFinalInputRoots(c,pinned);err==nil{t.Fatal("compiled Params slice mutation accepted under stale JSON")}
}
