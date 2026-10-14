package v2optical

import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

type perspectiveReplacementDraftSnapshot struct {
 SchemaVersion int `json:"schema_version"`
 Identity NegativeReplacementPublicationIdentity `json:"identity"`
 IdentitySHA string `json:"identity_sha"`
 MarkerJCS string `json:"marker_jcs"`
 Draft PerspectiveDraftSnapshot `json:"draft"`
 InputDigest string `json:"input_digest"`
}

// PerspectiveReplacementDraft is owned preliminary compiler output. Ordinary
// perspective persistence must not accept its replacement run/task identities.
// Dedicated SQL scope, physical locks and publication remain mandatory.
type PerspectiveReplacementDraft struct {raw string}
func(d PerspectiveReplacementDraft)Bytes()[]byte{return []byte(d.raw)}

func(p PreparedMappedReplacementStep)CompilePerspectiveReplacementDraft(ctx context.Context,snapshot uuid.UUID,pin PerspectiveQualificationPin,locks v2qualification.DeploymentLocks,report,calibration []byte,maxPixels int64,capacity PerspectiveResourceEstimate,lineRef *v2input.Ref,lines ...[]byte)(PerspectiveReplacementDraft,error){
 var zero PerspectiveReplacementDraft
 if err:=ctx.Err();err!=nil{return zero,err}
 if snapshot==uuid.Nil{return zero,v2input.ErrInvalid}
 identity,digest,err:=p.PublicationIdentity(ctx,lineRef);if err!=nil{return zero,err}
 if identity.Operation!="optical.perspective"{return zero,v2input.ErrInvalid}
 plan,qualified,err:=p.CompilePerspective(ctx,pin,locks,report,calibration,maxPixels,capacity,lineRef,lines...);if err!=nil{return zero,err}
 if plan.Operation!=identity.Operation||plan.ParamsSHA!=identity.ParamsSHA{return zero,v2input.ErrInvalid}
 params,err:=ParsePerspectiveParams(plan.Params);if err!=nil{return zero,err}
 source,err:=p.sourceDocument(ctx,params.Base.Source);if err!=nil{return zero,err}
 entries,_,err:=PerspectiveTaskInputEntries(ctx,plan,source,qualified,v2input.Ref{ArtifactID:identity.ParamsDocument,SHA256:identity.ParamsSHA});if err!=nil{return zero,err}
 inputDigest,err:=opticalDraftInputDigest(identity.Task,identity.Operation,entries);if err!=nil{return zero,err}
 marker,markerSHA,err:=p.RunMarker(ctx);if err!=nil{return zero,err}
 if markerSHA!=identity.MarkerSHA{return zero,v2input.ErrInvalid}
 who:=p.parents.step.intent.who
 draft:=PerspectiveDraftSnapshot{Tenant:identity.Tenant,Project:identity.Project,Actor:who.UserID,Session:who.SessionID,SnapshotID:snapshot,Run:identity.Run,Workflow:identity.Workflow,Task:identity.Task,ParamsDocument:identity.ParamsDocument,ParamsJCS:string(plan.Params),ParamsSHA:identity.ParamsSHA,LinesJCS:plan.LinesJCS,SourceJCS:string(source),Profile:append(json.RawMessage(nil),plan.Profile...),MaxPixels:maxPixels,ReservedBytes:plan.ReservedBytes}
 raw,err:=canonical.Marshal(perspectiveReplacementDraftSnapshot{SchemaVersion:2,Identity:identity,IdentitySHA:digest,MarkerJCS:string(marker),Draft:draft,InputDigest:inputDigest});if err!=nil{return zero,err}
 if len(raw)>3<<20{return zero,v2input.ErrInvalid}
 return PerspectiveReplacementDraft{raw:string(raw)},ctx.Err()
}

// Restore recompiles against current physical inputs and current qualification;
// a saved profile or caller-supplied packet cannot replace those authorities.
func(p PreparedMappedReplacementStep)RestorePerspectiveReplacementDraft(ctx context.Context,raw []byte,snapshot uuid.UUID,pin PerspectiveQualificationPin,locks v2qualification.DeploymentLocks,report,calibration []byte,maxPixels int64,capacity PerspectiveResourceEstimate,lineRef *v2input.Ref,lines ...[]byte)(PerspectiveReplacementDraft,error){
 var zero PerspectiveReplacementDraft
 if err:=ctx.Err();err!=nil{return zero,err}
 if snapshot==uuid.Nil||len(raw)<2||len(raw)>3<<20{return zero,v2input.ErrInvalid}
 var saved perspectiveReplacementDraftSnapshot
 if json.Unmarshal(raw,&saved)!=nil||saved.SchemaVersion!=2||saved.Draft.SnapshotID!=snapshot{return zero,v2input.ErrInvalid}
 exact,err:=canonical.Marshal(saved);if err!=nil||!bytes.Equal(raw,exact){return zero,v2input.ErrInvalid}
 fresh,err:=p.CompilePerspectiveReplacementDraft(ctx,snapshot,pin,locks,report,calibration,maxPixels,capacity,lineRef,lines...);if err!=nil{return zero,err}
 if !bytes.Equal(raw,fresh.Bytes()){return zero,v2input.ErrInvalid}
 return fresh,ctx.Err()
}
