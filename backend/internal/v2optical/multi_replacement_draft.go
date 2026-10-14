package v2optical

import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

type multiReplacementDraftSnapshot struct {
 SchemaVersion int `json:"schema_version"`
 Identity NegativeReplacementPublicationIdentity `json:"identity"`
 IdentitySHA string `json:"identity_sha"`
 MarkerJCS string `json:"marker_jcs"`
 Draft MultiDraftSnapshot `json:"draft"`
 InputDigest string `json:"input_digest"`
}
// MultiReplacementDraft owns preliminary recovery bytes, never ordinary multi
// admission authority. Persistence must repeat current physical/SQL proofs.
type MultiReplacementDraft struct{raw string}
func(d MultiReplacementDraft) Bytes()[]byte{return []byte(d.raw)}

func(p PreparedMappedReplacementStep) CompileMultiReplacementDraft(ctx context.Context,snapshot uuid.UUID,pin MultiQualificationPin,locks v2qualification.DeploymentLocks,report,calibration []byte,capacity MultiResourceEstimate)(MultiReplacementDraft,error){
 var zero MultiReplacementDraft
 if err:=ctx.Err();err!=nil{return zero,err};if snapshot==uuid.Nil{return zero,v2input.ErrInvalid}
 identity,digest,err:=p.PublicationIdentity(ctx,nil);if err!=nil{return zero,err}
 if identity.Operation!="optical.panorama"&&identity.Operation!="optical.focus_stack"{return zero,v2input.ErrInvalid}
 plan,qualified,err:=p.CompileMulti(ctx,pin,locks,report,calibration,capacity);if err!=nil{return zero,err}
 var frames []v2input.SessionVersionBinding
 if identity.Operation=="optical.panorama"{params,err:=ParsePanoramaParams(plan.Params);if err!=nil{return zero,err};frames=params.Frames
 }else{params,err:=ParseFocusStackParams(plan.Params);if err!=nil{return zero,err};frames=params.Frames}
 originals:=make([][]byte,len(frames))
 for i,frame:=range frames{originals[i],err=p.sourceDocument(ctx,frame.Source);if err!=nil{return zero,err}}
 marker,markerSHA,err:=p.RunMarker(ctx);if err!=nil{return zero,err};if markerSHA!=identity.MarkerSHA{return zero,v2input.ErrInvalid}
 who:=p.parents.step.intent.who
 return compileMultiReplacementDraftPacket(ctx,identity,digest,snapshot,who.UserID,who.SessionID,marker,plan,qualified,originals)
}

// Private serialization boundary; the public entrypoint obtains every input
// from the ready mapped step and freshly qualified physical sources above.
func compileMultiReplacementDraftPacket(ctx context.Context,identity NegativeReplacementPublicationIdentity,digest string,snapshot,actor,session uuid.UUID,marker []byte,plan MultiTaskPlan,qualified VerifiedMultiQualification,originals [][]byte)(MultiReplacementDraft,error){
 var zero MultiReplacementDraft
 if err:=ctx.Err();err!=nil{return zero,err}
 if identity.SchemaVersion!=2||identity.Tenant==uuid.Nil||identity.Project==uuid.Nil||identity.Review==uuid.Nil||identity.OriginalRun==uuid.Nil||identity.OriginalTask==uuid.Nil||snapshot==uuid.Nil||actor==uuid.Nil||session==uuid.Nil||plan.Operation!=identity.Operation||plan.ParamsSHA!=identity.ParamsSHA{return zero,v2input.ErrInvalid}
 if identity.Operation!="optical.panorama"&&identity.Operation!="optical.focus_stack"{return zero,v2input.ErrInvalid}
 if _,err:=domain.ParseDigest(identity.DecisionHash);err!=nil{return zero,v2input.ErrInvalid}
 namespace:=uuid.NewSHA1(identity.Tenant,[]byte("V29.NegativeReplacement/v2:"+identity.Project.String()+":"+identity.Review.String()+":"+identity.DecisionHash))
 run:=uuid.NewSHA1(namespace,[]byte("run:"+identity.OriginalRun.String()+":"+identity.OriginalTask.String()));task:=uuid.NewSHA1(namespace,[]byte("task:"+identity.OriginalTask.String()));workflow,_:=v2input.WorkflowID(run)
 if identity.Run!=run||identity.Task!=task||identity.Workflow!=workflow||identity.ParamsDocument!=uuid.NewSHA1(task,[]byte("params")){return zero,v2input.ErrInvalid}
 expected,err:=canonical.Hash(identity);if err!=nil||expected!=digest{return zero,v2input.ErrInvalid}
 markerHash,err:=canonical.Hash(json.RawMessage(marker));if err!=nil||markerHash!=identity.MarkerSHA{return zero,v2input.ErrInvalid}
 entries,_,err:=MultiTaskInputEntries(ctx,plan,originals,qualified,v2input.Ref{ArtifactID:identity.ParamsDocument,SHA256:identity.ParamsSHA});if err!=nil{return zero,err}
 inputDigest,err:=opticalDraftInputDigest(identity.Task,identity.Operation,entries);if err!=nil{return zero,err}
 sources:=make([]string,len(originals));for i,raw:=range originals{sources[i]=string(raw)}
 draft:=MultiDraftSnapshot{Tenant:identity.Tenant,Project:identity.Project,Actor:actor,Session:session,SnapshotID:snapshot,Operation:identity.Operation,Run:identity.Run,Workflow:identity.Workflow,Task:identity.Task,ParamsDocument:identity.ParamsDocument,ParamsJCS:string(plan.Params),ParamsSHA:plan.ParamsSHA,SourcesJCS:sources,Profile:append(json.RawMessage(nil),plan.Profile...),ReservedBytes:plan.ReservedBytes}
 raw,err:=canonical.Marshal(multiReplacementDraftSnapshot{SchemaVersion:2,Identity:identity,IdentitySHA:digest,MarkerJCS:string(marker),Draft:draft,InputDigest:inputDigest});if err!=nil||len(raw)>40<<20{return zero,v2input.ErrInvalid}
 return MultiReplacementDraft{raw:string(raw)},ctx.Err()
}
func(p PreparedMappedReplacementStep) RestoreMultiReplacementDraft(ctx context.Context,raw []byte,snapshot uuid.UUID,pin MultiQualificationPin,locks v2qualification.DeploymentLocks,report,calibration []byte,capacity MultiResourceEstimate)(MultiReplacementDraft,error){
 var zero MultiReplacementDraft
 if err:=ctx.Err();err!=nil{return zero,err};if snapshot==uuid.Nil||len(raw)<2||len(raw)>40<<20{return zero,v2input.ErrInvalid}
 var saved multiReplacementDraftSnapshot
 if json.Unmarshal(raw,&saved)!=nil||saved.SchemaVersion!=2||saved.Draft.SnapshotID!=snapshot{return zero,v2input.ErrInvalid}
 exact,err:=canonical.Marshal(saved);if err!=nil||!bytes.Equal(raw,exact){return zero,v2input.ErrInvalid}
 fresh,err:=p.CompileMultiReplacementDraft(ctx,snapshot,pin,locks,report,calibration,capacity);if err!=nil{return zero,err}
 if !bytes.Equal(raw,fresh.Bytes()){return zero,v2input.ErrInvalid};return fresh,ctx.Err()
}
