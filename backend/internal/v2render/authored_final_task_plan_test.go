package v2render

import (
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "os"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

// Detached compiler controls only. Synthetic typed evidence here is not an
// object/catalog proof or an enabled/qualified compose/2 worker installation.
func authoredFinalTaskCompilerFixture(t *testing.T)(AuthoredFinalInputEvidence,v2input.Ref,AuthoredFinalExecutionProfile){
 t.Helper();raw,err:=os.ReadFile("testdata/authored_final_params.json");if err!=nil{t.Fatal(err)}
 var params AuthoredFinalParams;if json.Unmarshal(raw,&params)!=nil{t.Fatal("params fixture")}
 compiled,err:=CompileAuthoredFinalParams(params);if err!=nil{t.Fatal(err)}
 sum:=sha256.Sum256(compiled.JSON);ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:hex.EncodeToString(sum[:])}
 roots,err:=AuthoredFinalInputRoots(compiled,ref);if err!=nil{t.Fatal(err)}
 pin:=strings.Repeat("a",64)
 profile:=AuthoredFinalExecutionProfile{Backend:"cpu",Renderer:"v2.2",KernelVersion:"v2.authored.1",Operation:"render.compose",Version:"2",ContractsSHA:AuthoredFinalSchemasSHA,RuntimeSHA:pin,ModelSHA:pin,ICCSHA:pin,TileSize:512,AlphaMode:"require_float32",RequiredReview:true,
 Resources:LayerResourceEstimate{RAMBytes:10,ScratchBytes:10,OutputBytes:10},Qualification:RenderQualificationPin{RenderQualification:RenderQualification{ModelSHA:pin,RuntimeSHA:pin,Backend:"cpu",DeviceSHA:pin,DatasetSHA:pin},CalibrationSHA:pin}}
 return AuthoredFinalInputEvidence{Params:compiled,Inputs:v2draft.TaskInputEvidence{Entries:roots,Occurrences:make([]v2draft.OccurrenceWitness,len(roots))}},ref,profile
}
func TestAuthoredFinalTaskCPUTruthAndPhotographicCapacity(t *testing.T){
 evidence,ref,profile:=authoredFinalTaskCompilerFixture(t);raw,_:=canonical.Marshal(profile)
 plan,err:=compileAuthoredFinalTask(evidence,ref,raw)
 if err!=nil||plan.Task.ResourceClass!="gpu"||plan.Task.OperationVersion!="2"{t.Fatal("compose2 photographic task",err)}
 var actual AuthoredFinalExecutionProfile
 if json.Unmarshal(plan.Task.ExecutionProfile,&actual)!=nil||actual.Backend!="cpu"||actual.Resources.VRAMBytes!=0||actual.Provider.VRAMBytes!=0||!actual.RequiredReview{t.Fatal("CPU compute truth/review lost")}
}
func TestAuthoredFinalTaskRejectsBackendAndRootSubstitution(t *testing.T){
 for _,name:=range []string{"gpu-label","provider-vram","task-vram","qualification-backend","qualification-runtime","no-review","old-generation","missing-current-root","params-sha","tile","alpha","transport-version","kernel-version"}{t.Run(name,func(t *testing.T){
 evidence,ref,profile:=authoredFinalTaskCompilerFixture(t)
 switch name{
 case "gpu-label":profile.Backend="mps"
 case "provider-vram":profile.Provider.VRAMBytes=1
 case "task-vram":profile.Resources.VRAMBytes=1
 case "qualification-backend":profile.Qualification.Backend="cuda"
 case "qualification-runtime":profile.Qualification.RuntimeSHA=strings.Repeat("b",64)
 case "no-review":profile.RequiredReview=false
 case "old-generation":profile.ContractsSHA=strings.Repeat("b",64)
 case "missing-current-root":for i,e:=range evidence.Inputs.Entries{if e.Ref==evidence.Params.Params.Source.Source{evidence.Inputs.Entries=append(evidence.Inputs.Entries[:i],evidence.Inputs.Entries[i+1:]...);break}}
 case "params-sha":ref.SHA256=strings.Repeat("b",64)
 case "tile":profile.TileSize=1
 case "alpha":profile.AlphaMode="ignore"
 case "transport-version":profile.Renderer="v2.authored.1"
 case "kernel-version":profile.KernelVersion="v2.2"
 }
 raw,_:=canonical.Marshal(profile);if _,err:=compileAuthoredFinalTask(evidence,ref,raw);err==nil{t.Fatal("substituted task accepted")}
 })}
}
