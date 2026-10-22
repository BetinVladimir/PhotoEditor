package v2region

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type FrozenAuthoredCropNode struct{body []byte;plan AuthoredCropPlan;inputs FrozenRegionInputs}
func(n FrozenAuthoredCropNode)JSON()[]byte{return append([]byte(nil),n.body...)}

// CompileNode checks a server-installed, undecorated CPU presentation profile.
// The full render keeps its independent qualified GPU/CPU backend. This node
// cannot replace it with a CPU string or confer renderer qualification.
func(p AuthoredCropPlan)CompileNode(task uuid.UUID,params v2input.Ref,resolved []v2input.Entry,profile []byte)(FrozenAuthoredCropNode,error){
 invalid:=func()(FrozenAuthoredCropNode,error){return FrozenAuthoredCropNode{},ErrRegionInvalid}
 body,err:=canonical.Bytes(profile);if err!=nil||len(body)>65536{return invalid()}
 var fields map[string]json.RawMessage
 if json.Unmarshal(body,&fields)!=nil||fields==nil{return invalid()}
 for _,key:=range []string{"contracts_sha","v2_input_manifest","v2_entry_count","v2_entries_sha"}{if _,ok:=fields[key];ok{return invalid()}}
 value:=func(key string)string{var v string;_ = json.Unmarshal(fields[key],&v);return v}
 if value("backend")!="cpu"||value("renderer_version")!="v2.2"||value("authored_region_intent_sha256")!=p.params.IntentSHA||value("icc_lock_sha")!=p.intent.PresentationICCLockSHA||value("runtime_lock_sha")!=p.intent.PresentationRuntimeSHA{return invalid()}
 var inputBytes int64;var admitted AuthoredCropResources
 if json.Unmarshal(fields["authored_region_input_bytes"],&inputBytes)!=nil||json.Unmarshal(fields["resource_estimate"],&admitted)!=nil{return invalid()}
 required,err:=CompileAuthoredCropResources(p.rendered.Canvas.Width,p.rendered.Canvas.Height,inputBytes,p.params.MaxEdge,p.rendered.Alpha!=nil)
 if err!=nil||admitted.PixelCount!=required.PixelCount||admitted.VRAMBytes!=0||admitted.RAMBytes<required.RAMBytes||admitted.ScratchBytes<required.ScratchBytes||admitted.OutputBytes<required.OutputBytes{return invalid()}
 inputs,err:=p.FreezeInputs(task,params,resolved);if err!=nil{return invalid()}
 node,err:=canonical.Marshal(regionNodeDocument{task,params,string(p.body),inputs.InputDigest,json.RawMessage(body)});if err!=nil{return invalid()}
 return FrozenAuthoredCropNode{node,p,inputs},nil
}

// DraftShell uses the existing blocked-shell / real input publication / graph
// seal protocol. Authority is repeated by Begin/Seal below; no object IO occurs
// while their SQL transaction is open.
func(n FrozenAuthoredCropNode)DraftShell(snapshot uuid.UUID)(v2input.DraftShell,[]v2input.TaskSource,string,error){
 invalid:=func()(v2input.DraftShell,[]v2input.TaskSource,string,error){return v2input.DraftShell{},nil,"",ErrRegionInvalid}
 if snapshot==uuid.Nil||n.inputs.Open==nil{return invalid()}
 p:=n.plan;key,err:=p.intent.RequestChildKey(p.params.Request,"crop");if err!=nil{return invalid()}
 run,err:=v2input.RunID(p.params.Tenant,p.intent.Actor,key);if err!=nil{return invalid()}
 workflow,err:=v2input.WorkflowID(run);if err!=nil{return invalid()}
 task,err:=v2input.TaskID(workflow,"region.authored_crop",0);if err!=nil{return invalid()}
 var node regionNodeDocument
 if json.Unmarshal(n.body,&node)!=nil||node.TaskID!=task||node.InputDigest!=n.inputs.InputDigest{return invalid()}
 var profile struct{ResourceEstimate AuthoredCropResources `json:"resource_estimate"`}
 if json.Unmarshal(node.Profile,&profile)!=nil{return invalid()}
 config,err:=canonical.Marshal(struct{
  Project uuid.UUID `json:"project_id"`;Snapshot uuid.UUID `json:"input_snapshot_id"`;Crop any `json:"authored_region_crop"`
 }{p.params.Project,snapshot,struct{
  SchemaVersion int `json:"schema_version"`;Request uuid.UUID `json:"request_id"`;IntentSHA string `json:"intent_sha256"`;Session uuid.UUID `json:"auth_session_id"`;Nodes []json.RawMessage `json:"nodes"`
 }{1,p.params.Request,p.params.IntentSHA,p.intent.Session,[]json.RawMessage{n.body}}})
 if err!=nil{return invalid()};configSHA,err:=canonical.Hash(json.RawMessage(config));if err!=nil{return invalid()}
 nodeKey,_:=v2input.NodeKey("region.authored_crop",0)
 shell:=v2input.DraftShell{Tenant:p.params.Tenant,Run:run,WorkflowID:workflow,Project:p.params.Project,Snapshot:snapshot,Actor:p.intent.Actor,Workflow:"preview",PolicySHA:p.intent.RenderPolicySHA,ConfigSHA:configSHA,Config:config,Bytes:profile.ResourceEstimate.OutputBytes,
  Tasks:[]v2input.DraftTask{{ID:task,NodeKey:nodeKey,Operation:"region.authored_crop",OperationVersion:"1",ResourceClass:"cpu",ParamsArtifactID:node.Params.ArtifactID,ParamsSHA:node.Params.SHA256,InputDigest:node.InputDigest,ExecutionProfile:node.Profile}},Edges:[]v2input.DraftEdge{}}
 return shell,[]v2input.TaskSource{{ID:task,Open:n.inputs.Open,Direct:append([]v2input.Ref(nil),n.inputs.Direct...)}},key,nil
}
func(n FrozenAuthoredCropNode)BeginInTransaction(ctx context.Context,tx pgx.Tx,snapshot uuid.UUID)(string,uuid.UUID,error){
 shell,_,key,err:=n.DraftShell(snapshot);if err!=nil||tx==nil{return "",uuid.Nil,ErrRegionInvalid}
 var live bool
 if err=tx.QueryRow(ctx,"SELECT photo.authored_region_crop_admission_scope($1,$2,$3,$4)",shell.Tenant,n.plan.params.Request,shell.Run,json.RawMessage(n.body)).Scan(&live);err!=nil{return "",uuid.Nil,err}
 if !live{return "",uuid.Nil,v2input.ErrExpired}
 return v2input.BeginAdmissionInTransaction(ctx,tx,shell,key,n.plan.sha)
}
func(n FrozenAuthoredCropNode)SealInTransaction(ctx context.Context,tx pgx.Tx,snapshot uuid.UUID,built v2input.Built)error{
 shell,sources,_,err:=n.DraftShell(snapshot);if err!=nil||tx==nil{return ErrRegionInvalid}
 var live bool
 if err=tx.QueryRow(ctx,"SELECT photo.authored_region_crop_seal_scope($1,$2,$3,$4)",shell.Tenant,n.plan.params.Request,shell.Run,json.RawMessage(n.body)).Scan(&live);err!=nil{return err}
 if !live{return v2input.ErrExpired}
 return v2input.SealGraphInTransaction(ctx,tx,shell.Tenant,shell.Run,[]v2input.GraphTask{{ID:sources[0].ID,Built:built,Open:sources[0].Open,Direct:sources[0].Direct}})
}
// Exact actor/session identity accompanies recovery, never a GUC-only shortcut.
func(n FrozenAuthoredCropNode)Identity()auth.Identity{return auth.Identity{UserID:n.plan.intent.Actor,SessionID:n.plan.intent.Session}}
