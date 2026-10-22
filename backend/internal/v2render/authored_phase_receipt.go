package v2render

import (
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)

// AuthoredPhaseTask is an observation of a real canonical producer, including
// the exact ordered root vector. It never supplies task identity or admission.
type AuthoredPhaseTask struct {
 RunID uuid.UUID `json:"producer_run_id"`
 TaskID uuid.UUID `json:"task_id"`
 ResultID uuid.UUID `json:"result_id"`
 Operation string `json:"operation"`
 Version string `json:"operation_version"`
 Attempt int `json:"attempt"`
 Fence int64 `json:"fence"`
 Params v2input.Ref `json:"params"`
 InputDigest string `json:"input_digest"`
 ProfileJCS string `json:"execution_profile_jcs"`
 ProfileSHA string `json:"execution_profile_sha256"`
 Outputs []v2input.Ref `json:"outputs"`
}
// AuthoredGeometryPhaseWitness retains the independently completed native
// proposal, distinct from the later authored blend producer and its output.
type AuthoredGeometryPhaseWitness struct {
 Task AuthoredPhaseTask `json:"task"`
 Root v2input.Ref `json:"root"`
 Image v2input.Ref `json:"image"`
 Report v2input.Ref `json:"report"`
}
type AuthoredPhaseReceipt struct {
 SchemaVersion int `json:"schema_version"`
 Tenant uuid.UUID `json:"tenant_id"`
 Parent uuid.UUID `json:"parent_run_id"`
 Instruction uuid.UUID `json:"instruction_id"`
 Ordinal int `json:"ordinal"`
 Kind string `json:"kind"`
 ProgramSHA string `json:"program_sha256"`
 PredecessorSHA string `json:"predecessor_sha256"`
 OriginalRecipe v2input.Ref `json:"original_recipe"`
 InputSource v2input.SessionVersionBinding `json:"input_source"`
 OutputSource v2input.SessionVersionBinding `json:"output_source"`
 Result v2input.Ref `json:"result"`
 Report v2input.Ref `json:"report"`
 PhysicalClosure []v2input.Ref `json:"physical_closure"`
 Geometry *AuthoredGeometryPhaseWitness `json:"native,omitempty"`
 AuthoredPhaseTask
}

// CompileAuthoredPhaseReceipt follows physical typed ImageResult/WorkingImage /
// report verification and independent native replay. SQL repeats the producer,
// source, ordered-output and current user scope before appending this body.
// This content compiler deliberately cannot mark a run complete or accepted.
func CompileAuthoredPhaseReceipt(source AuthoredInstructionSource,task AuthoredPhaseTask,
 output v2input.SessionVersionBinding,result,report v2input.Ref,geometry *AuthoredGeometryPhaseWitness,physicalClosure []v2input.Ref)([]byte,string,error){
 fail:=func()([]byte,string,error){return nil,"",v2input.ErrInvalid}
 if source.Tenant==uuid.Nil||source.Parent==uuid.Nil||source.Instruction.Ordinal<1||
 task.RunID==uuid.Nil||task.RunID==source.Parent||task.TaskID==uuid.Nil||task.ResultID==uuid.Nil||
 task.Attempt<1||task.Fence<1||!authoredBindingValid(source.Source)||!authoredBindingValid(output)||
 output.AssetVersionID!=source.Source.AssetVersionID||len(task.Outputs)==0||len(task.Outputs)>128{return fail()}
 validRef:=func(ref v2input.Ref)bool{_,err:=domain.ParseDigest(ref.SHA256);return ref.ArtifactID!=uuid.Nil&&err==nil}
 if !validRef(task.Params)||!validRef(result)||!validRef(report)||!validRef(source.OriginalRecipe){return fail()}
 for _,sha:=range []string{task.InputDigest,task.ProfileSHA,source.ProgramSHA,source.PredecessorSHA}{if _,err:=domain.ParseDigest(sha);err!=nil{return fail()}}
 raw,err:=canonical.Bytes([]byte(task.ProfileJCS));if err!=nil||string(raw)!=task.ProfileJCS{return fail()}
 var profile map[string]json.RawMessage;if json.Unmarshal(raw,&profile)!=nil||len(profile)==0{return fail()}
 digest:=sha256.Sum256(raw);if hex.EncodeToString(digest[:])!=task.ProfileSHA{return fail()}
 seen:=map[uuid.UUID]bool{};found:=false
 for _,ref:=range task.Outputs{if !validRef(ref)||seen[ref.ArtifactID]{return fail()};seen[ref.ArtifactID]=true;if ref==result{found=true}}
 if !found{return fail()}
 if len(physicalClosure)<1||len(physicalClosure)>4096{return fail()}
 closureSeen:=map[uuid.UUID]bool{};required:=map[v2input.Ref]bool{result:false,report:false,output.Source:false}
 for _,ref:=range physicalClosure{if !validRef(ref)||closureSeen[ref.ArtifactID]{return fail()};closureSeen[ref.ArtifactID]=true;if _,ok:=required[ref];ok{required[ref]=true}}
 for _,found:=range required{if !found{return fail()}}
 switch source.Instruction.Kind{
 case "geometry":
  if task.Operation!="render.authored_geometry"||task.Version!="1"||geometry==nil||geometry.Task.RunID==task.RunID||geometry.Task.RunID==source.Parent{return fail()}
  native:=geometry.Task
  if native.RunID==uuid.Nil||native.TaskID==uuid.Nil||native.ResultID==uuid.Nil||native.TaskID==task.TaskID||native.Attempt<1||native.Fence<1||native.Version!="1"||!validRef(native.Params)||!validRef(geometry.Root)||!validRef(geometry.Image)||!validRef(geometry.Report)||len(native.Outputs)==0||len(native.Outputs)>128{return fail()}
  switch native.Operation{case "optical.lens","optical.perspective","optical.panorama","optical.focus_stack","image.upscale":default:return fail()}
  for _,sha:=range []string{native.InputDigest,native.ProfileSHA}{if _,err:=domain.ParseDigest(sha);err!=nil{return fail()}}
  raw,err:=canonical.Bytes([]byte(native.ProfileJCS));if err!=nil||string(raw)!=native.ProfileJCS{return fail()};hash:=sha256.Sum256(raw);if hex.EncodeToString(hash[:])!=native.ProfileSHA{return fail()};var nativeProfile map[string]json.RawMessage;if json.Unmarshal(raw,&nativeProfile)!=nil||len(nativeProfile)==0{return fail()}
  seenNative:=map[uuid.UUID]bool{};foundNative:=false
  for _,ref:=range native.Outputs{if !validRef(ref)||seenNative[ref.ArtifactID]{return fail()};seenNative[ref.ArtifactID]=true;if ref==geometry.Root{foundNative=true}}
  if !foundNative||result==geometry.Root||output.Source==geometry.Image{return fail()}
 case "segment":if geometry!=nil{return fail()};if task.Operation!="render.authored_segment"||task.Version!="1"||output.CanvasHash!=source.Source.CanvasHash{return fail()}
 case "finalize":if geometry!=nil{return fail()};if task.Operation!="render.compose"||task.Version!="2"||output.CanvasHash!=source.Source.CanvasHash{return fail()}
 default:return fail()
 }
 body,err:=canonical.Marshal(AuthoredPhaseReceipt{SchemaVersion:1,Tenant:source.Tenant,Parent:source.Parent,
 Instruction:source.Instruction.Identity,Ordinal:source.Instruction.Ordinal,Kind:source.Instruction.Kind,
 ProgramSHA:source.ProgramSHA,PredecessorSHA:source.PredecessorSHA,OriginalRecipe:source.OriginalRecipe,
 InputSource:source.Source,OutputSource:output,Result:result,Report:report,Geometry:geometry,PhysicalClosure:append([]v2input.Ref(nil),physicalClosure...),AuthoredPhaseTask:task})
 if err!=nil{return fail()};sum:=sha256.Sum256(body);return body,hex.EncodeToString(sum[:]),nil
}
