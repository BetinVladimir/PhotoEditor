package v2optical

import (
 "bytes"
 "context"
 "encoding/json"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// AutomaticStageDescriptor reconstructs the complete typed inventory/profile
// from the owned draft and current qualification. Physical locks and SQL stage
// authority are separate; this pure compiler cannot publish a task.
func(d PerspectiveReplacementDraft) AutomaticStageDescriptor(ctx context.Context,located LocatedPerspectiveQualification)([]byte,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 var saved perspectiveReplacementDraftSnapshot
 if len(d.raw)<2||len(d.raw)>3<<20||json.Unmarshal([]byte(d.raw),&saved)!=nil||saved.SchemaVersion!=2{return nil,v2input.ErrInvalid}
 exact,err:=canonical.Marshal(saved);if err!=nil||!bytes.Equal(exact,d.Bytes()){return nil,v2input.ErrInvalid}
 params,err:=ParsePerspectiveParams([]byte(saved.Draft.ParamsJCS));if err!=nil{return nil,err}
 if params.Mode!="auto"||params.Lines==nil{return nil,v2input.ErrInvalid}
 qualified,err:=located.Qualification();if err!=nil{return nil,err}
 var profile struct{Resources PerspectiveResourceEstimate `json:"resource_estimate"`}
 if json.Unmarshal(saved.Draft.Profile,&profile)!=nil{return nil,v2input.ErrInvalid}
 plan,err:=CompilePerspectiveTaskPlan(ctx,params.Base,[]byte(saved.Draft.ParamsJCS),[]byte(saved.Draft.SourceJCS),qualified,saved.Draft.MaxPixels,profile.Resources,[]byte(saved.Draft.LinesJCS));if err!=nil{return nil,err}
 if plan.ParamsSHA!=saved.Identity.ParamsSHA||plan.ReservedBytes!=saved.Draft.ReservedBytes||!bytes.Equal(plan.Profile,saved.Draft.Profile){return nil,v2input.ErrInvalid}
 ref:=v2input.Ref{ArtifactID:saved.Identity.ParamsDocument,SHA256:saved.Identity.ParamsSHA}
 entries,_,err:=PerspectiveTaskInputEntries(ctx,plan,[]byte(saved.Draft.SourceJCS),qualified,ref);if err!=nil{return nil,err}
 digest,err:=opticalDraftInputDigest(saved.Identity.Task,"optical.perspective",entries);if err!=nil{return nil,err}
 if digest!=saved.InputDigest{return nil,v2input.ErrInvalid}
 task:=v2input.DraftTask{ID:saved.Identity.Task,NodeKey:"optical.perspective:0",Operation:"optical.perspective",OperationVersion:"1",ResourceClass:"cpu",ParamsArtifactID:ref.ArtifactID,ParamsSHA:ref.SHA256,InputDigest:digest,ExecutionProfile:plan.Profile}
 raw,err:=canonical.Marshal(struct{SchemaVersion int `json:"schema_version"`;Task v2input.DraftTask `json:"task"`;Entries []v2input.Entry `json:"entries"`;ReservedBytes int64 `json:"reserved_bytes"`}{1,task,entries,saved.Draft.ReservedBytes})
 if err!=nil{return nil,err};if len(raw)>1<<20{return nil,v2input.ErrInvalid};return raw,ctx.Err()
}

// BuildAutomaticTransport uses the same validated compiler and bounded manifest
// protocol; publisher must enforce automatic replacement saved-draft authority.
func(d PerspectiveReplacementDraft) BuildAutomaticTransport(ctx context.Context,located LocatedPerspectiveQualification,publisher v2input.Publisher)(v2input.GraphTask,error){
 var zero v2input.GraphTask
 if publisher==nil{return zero,v2input.ErrInvalid}
 if _,err:=d.AutomaticStageDescriptor(ctx,located);err!=nil{return zero,err}
 var saved perspectiveReplacementDraftSnapshot
 if json.Unmarshal(d.Bytes(),&saved)!=nil{return zero,v2input.ErrInvalid}
 params,err:=ParsePerspectiveParams([]byte(saved.Draft.ParamsJCS));if err!=nil{return zero,err}
 qualified,err:=located.Qualification();if err!=nil{return zero,err}
 var profile struct{Resources PerspectiveResourceEstimate `json:"resource_estimate"`}
 if json.Unmarshal(saved.Draft.Profile,&profile)!=nil{return zero,v2input.ErrInvalid}
 plan,err:=CompilePerspectiveTaskPlan(ctx,params.Base,[]byte(saved.Draft.ParamsJCS),[]byte(saved.Draft.SourceJCS),qualified,saved.Draft.MaxPixels,profile.Resources,[]byte(saved.Draft.LinesJCS));if err!=nil{return zero,err}
 return BuildPerspectiveTaskTransport(ctx,saved.Identity.Task,plan,[]byte(saved.Draft.SourceJCS),qualified,v2input.Ref{ArtifactID:saved.Identity.ParamsDocument,SHA256:saved.Identity.ParamsSHA},publisher)
}

func(d PerspectiveReplacementDraft) RestoreAutomaticTransport(ctx context.Context,located LocatedPerspectiveQualification,raw []byte)(v2input.GraphTask,error){
 return restoreOpticalPublishedTransport(ctx,raw,func(ctx context.Context)([]byte,error){return d.AutomaticStageDescriptor(ctx,located)},5)
}

// The wire acknowledgement is shared across perspective modes. It proves
// historical admission only, never current successful execution or Apply.
type AutomaticPerspectiveReplacementReceipt = ManualPerspectiveReplacementReceipt
func(d PerspectiveReplacementDraft) ParseAutomaticQueuedReceipt(ctx context.Context,located LocatedPerspectiveQualification,raw []byte)(AutomaticPerspectiveReplacementReceipt,error){
 var zero AutomaticPerspectiveReplacementReceipt
 descriptor,err:=d.AutomaticStageDescriptor(ctx,located);if err!=nil{return zero,err}
 var saved perspectiveReplacementDraftSnapshot
 if json.Unmarshal(d.Bytes(),&saved)!=nil{return zero,v2input.ErrInvalid}
 qualified,err:=located.Qualification();if err!=nil{return zero,err}
 pin,err:=qualified.Pin();if err!=nil{return zero,err}
 return parsePerspectiveReplacementReceipt(ctx,saved,descriptor,pin,raw,5)
}
