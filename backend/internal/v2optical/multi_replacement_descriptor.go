package v2optical

import (
 "bytes"
 "context"
 "encoding/json"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// Descriptor reconstructs the exact typed inventory and fresh profile from
// qualified bytes. It is pure compiler output, not current SQL admission proof.
func(d MultiReplacementDraft) Descriptor(ctx context.Context,qualified VerifiedMultiQualification,published v2input.Ref)([]byte,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if len(d.raw)<2||len(d.raw)>40<<20{return nil,v2input.ErrInvalid}
 var packet multiReplacementDraftSnapshot
 if json.Unmarshal([]byte(d.raw),&packet)!=nil||packet.SchemaVersion!=2{return nil,v2input.ErrInvalid}
 exact,err:=canonical.Marshal(packet);if err!=nil||!bytes.Equal(exact,[]byte(d.raw)){return nil,v2input.ErrInvalid}
 s:=packet.Draft
 if published.ArtifactID!=s.ParamsDocument||published.SHA256!=s.ParamsSHA{return nil,v2input.ErrInvalid}
 originals:=make([][]byte,len(s.SourcesJCS));for i,raw:=range s.SourcesJCS{originals[i]=[]byte(raw)}
 var header struct{Resources MultiResourceEstimate `json:"resource_estimate"`}
 if json.Unmarshal(s.Profile,&header)!=nil{return nil,v2input.ErrInvalid}
 plan,err:=CompileMultiTaskPlan(ctx,s.Operation,[]byte(s.ParamsJCS),originals,qualified,header.Resources);if err!=nil{return nil,err}
 fresh,err:=compileMultiReplacementDraftPacket(ctx,packet.Identity,packet.IdentitySHA,s.SnapshotID,s.Actor,s.Session,[]byte(packet.MarkerJCS),plan,qualified,originals)
 if err!=nil{return nil,err};if fresh.raw!=d.raw{return nil,v2input.ErrInvalid}
 entries,_,err:=MultiTaskInputEntries(ctx,plan,originals,qualified,published);if err!=nil{return nil,err}
 node,err:=v2input.NodeKey(s.Operation,0);if err!=nil{return nil,err}
 task:=v2input.DraftTask{ID:s.Task,NodeKey:node,Operation:s.Operation,OperationVersion:"1",ResourceClass:"cpu",ParamsArtifactID:published.ArtifactID,ParamsSHA:published.SHA256,InputDigest:packet.InputDigest,ExecutionProfile:append(json.RawMessage(nil),plan.Profile...)}
 raw,err:=canonical.Marshal(struct{
  SchemaVersion int `json:"schema_version"`
  Task v2input.DraftTask `json:"task"`
  Entries []v2input.Entry `json:"entries"`
  ReservedBytes int64 `json:"reserved_bytes"`
 }{1,task,entries,s.ReservedBytes})
 if err!=nil||len(raw)>1<<20{return nil,v2input.ErrInvalid}
 return raw,ctx.Err()
}
