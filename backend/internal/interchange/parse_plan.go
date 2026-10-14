package interchange

import (
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)

type ParseLimits struct {
 RAMBytes int64 `json:"ram_bytes"`
 ScratchBytes int64 `json:"scratch_bytes"`
 OutputBytes int64 `json:"output_bytes"`
 ExpandedBytes int64 `json:"expanded_bytes"`
 Files int `json:"files"`
}
type BundleEvidence struct {Ref v2input.Ref `json:"ref"`;Bytes int64 `json:"bytes"`}
type ParsePlan struct {
 queueClass string
 tenant uuid.UUID
 identity CreateIdentity
 params ParseParams
 paramsRaw,profile json.RawMessage
 paramsRef v2input.Ref
 archiveBytes int64
 outputBytes int64
}

// BundleEvidence must come from an authorized immutable artifact lookup. This
// pure compiler verifies binding/size, not database authority or physical bytes.
func CompileParsePlan(tenant uuid.UUID,who auth.Identity,request CreateRequest,key string,evidence BundleEvidence,limits ParseLimits)(ParsePlan,error){
 intent,err:=FreezeCreateIntent(tenant,who,request,key);if err!=nil{return ParsePlan{},err}
 if request.Direction!="import"||!supportedParseFormat(request.Format){return ParsePlan{},domain.NewError("INTERCHANGE_FORMAT_UNAVAILABLE",503)}
 if evidence.Ref!=*intent.Request.Bundle||evidence.Bytes<1||evidence.Bytes>64<<30||limits.ExpandedBytes<1||limits.ExpandedBytes>128<<30||limits.Files<1||limits.Files>30000{return ParsePlan{},domain.NewError("VALIDATION_FAILED",422)}
 params:=ParseParams{SchemaVersion:1,Request:ImportRequest{ProjectID:request.ProjectID,Direction:"import",Format:request.Format,Bundle:evidence.Ref},MaxExpandedBytes:limits.ExpandedBytes,MaxFiles:limits.Files}
 raw,err:=CompileParseParams(params);if err!=nil{return ParsePlan{},err}
 output:=limits.ExpandedBytes+(8<<20)
 scratch:=evidence.Bytes+output
 ram:=int64(1<<30)+int64(limits.Files)*2048
 if limits.OutputBytes<output||limits.ScratchBytes<scratch||limits.RAMBytes<ram{return ParsePlan{},domain.NewError("INTERCHANGE_RESOURCE_LIMIT",422)}
 profile,err:=canonical.Marshal(struct{
  Backend string `json:"backend"`;RendererVersion string `json:"renderer_version"`
  Estimate struct{RAM int64 `json:"ram_bytes"`;VRAM int64 `json:"vram_bytes"`;Scratch int64 `json:"scratch_bytes"`;Output int64 `json:"output_bytes"`;Pixels int64 `json:"pixel_count"`} `json:"resource_estimate"`
 }{Backend:"cpu",RendererVersion:"v2.2",Estimate:struct{RAM int64 `json:"ram_bytes"`;VRAM int64 `json:"vram_bytes"`;Scratch int64 `json:"scratch_bytes"`;Output int64 `json:"output_bytes"`;Pixels int64 `json:"pixel_count"`}{RAM:ram,Scratch:scratch,Output:output}})
 if err!=nil{return ParsePlan{},err}
 sum:=sha256.Sum256(raw)
 return ParsePlan{queueClass:"batch",tenant:tenant,identity:intent.Identity,params:params,paramsRaw:raw,profile:profile,paramsRef:v2input.Ref{ArtifactID:intent.Identity.Params,SHA256:hex.EncodeToString(sum[:])},archiveBytes:evidence.Bytes,outputBytes:output},nil
}
func(p ParsePlan)Identity()CreateIdentity{return p.identity}
func(p ParsePlan)Params()(ParseParams,v2input.Ref,[]byte){return p.params,p.paramsRef,append([]byte(nil),p.paramsRaw...)}
func(p ParsePlan)DirectInputs()[]v2input.Entry{return []v2input.Entry{{Ref:p.paramsRef,Contract:"Params"},{Ref:p.params.Request.Bundle,Contract:"InterchangeArchive"}}}
func(p ParsePlan)OutputBytes()int64{return p.outputBytes}

// Input digest is supplied only after transport publication, then independently
// checked by SQL registration/sealing. This method performs no publication.
func(p ParsePlan)DraftTask(inputDigest string)(v2input.DraftTask,error){
 if p.identity.Task==uuid.Nil||len(p.paramsRaw)==0{return v2input.DraftTask{},domain.NewError("VALIDATION_FAILED",422)}
 if _,err:=domain.ParseDigest(inputDigest);err!=nil{return v2input.DraftTask{},domain.NewError("VALIDATION_FAILED",422)}
 return v2input.DraftTask{ID:p.identity.Task,NodeKey:"v2.interchange.parse:0",Operation:"v2.interchange.parse",OperationVersion:"1",ResourceClass:"cpu",ParamsArtifactID:p.paramsRef.ArtifactID,ParamsSHA:p.paramsRef.SHA256,InputDigest:inputDigest,ExecutionProfile:append(json.RawMessage(nil),p.profile...)},nil
}
