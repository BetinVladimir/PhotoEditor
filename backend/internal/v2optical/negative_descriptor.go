package v2optical

import (
 "context"

 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// Descriptor reconstructs the blocked task from owned originals and an exact
// published Params pin. It grants neither publication nor SQL admission rights.
func(d NegativeDraft)Descriptor(ctx context.Context,qualified VerifiedNegativeQualification,published v2input.Ref)([]byte,error){
 plan,params,source,err:=d.RestorePlan(ctx,qualified);if err!=nil{return nil,err}
 if published!=params{return nil,v2input.ErrInvalid}
 snapshot,err:=d.Snapshot();if err!=nil{return nil,err}
 entries,_,err:=NegativeTaskInputEntries(ctx,plan,source,qualified,params);if err!=nil{return nil,err}
 node,err:=v2input.NodeKey("optical.negative",0);if err!=nil{return nil,err}
 task:=v2input.DraftTask{ID:snapshot.Owner.TaskID,NodeKey:node,Operation:"optical.negative",OperationVersion:"1",ResourceClass:"cpu",ParamsArtifactID:params.ArtifactID,ParamsSHA:params.SHA256,InputDigest:snapshot.InputDigest,ExecutionProfile:plan.Profile}
 raw,err:=canonical.Marshal(struct{
  SchemaVersion int `json:"schema_version"`
  Task v2input.DraftTask `json:"task"`
  Entries []v2input.Entry `json:"entries"`
  ReservedBytes int64 `json:"reserved_bytes"`
 }{1,task,entries,snapshot.ReservedBytes})
 if err!=nil||len(raw)>1<<20{return nil,v2input.ErrInvalid}
 return raw,ctx.Err()
}
