package v2optical

import (
 "bytes"
 "context"
 "encoding/json"
 "sort"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type negativeReplacementDiscoveryNode struct {
 Original uuid.UUID `json:"original_task_id"`
 Task uuid.UUID `json:"task_id"`
 Run uuid.UUID `json:"run_id"`
}

func decodeNegativeAdoptedReplacementNodes(raw []byte,expected []negativeReplacementDiscoveryNode)([]negativeReplacementDiscoveryNode,error){
 if len(raw)<2||len(raw)>1<<20||len(expected)>4096{return nil,v2input.ErrInvalid}
 normal,err:=canonical.Bytes(raw);if err!=nil{return nil,v2input.ErrInvalid}
 var out []negativeReplacementDiscoveryNode
 if json.Unmarshal(normal,&out)!=nil||out==nil||len(out)>len(expected){return nil,v2input.ErrInvalid}
 exact,err:=canonical.Marshal(out);if err!=nil||!bytes.Equal(normal,exact){return nil,v2input.ErrInvalid}
 known:=make(map[uuid.UUID]negativeReplacementDiscoveryNode,len(expected))
 for _,node:=range expected{if _,duplicate:=known[node.Original];duplicate{return nil,v2input.ErrInvalid};known[node.Original]=node}
 previous:=uuid.Nil
 for _,node:=range out{
  if node.Original==uuid.Nil||node.Task==uuid.Nil||node.Run==uuid.Nil||bytes.Compare(node.Original[:],previous[:])<=0||known[node.Original]!=node{return nil,v2input.ErrInvalid}
  previous=node.Original
 }
 return out,nil
}

func(p PreparedNegativeReplacementInputs)readAdoptedReplacementNodes(ctx context.Context,db NegativeTransactions,nodes []negativeReplacementDiscoveryNode)([]negativeReplacementDiscoveryNode,error){
 if err:=ctx.Err();err!=nil{return nil,err};if db==nil{return nil,v2input.ErrInvalid}
 body,err:=canonical.Marshal(nodes);if err!=nil{return nil,err}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=db.Begin(ctx);if err!=nil{return nil,err};if tx==nil{return nil,v2input.ErrInvalid}
 defer tx.Rollback(context.WithoutCancel(ctx))
 var role string
 if err=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil{return nil,err};if role!="photo_scheduler"{return nil,v2input.ErrInvalid}
 if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",p.tenant.String());err!=nil{return nil,err}
 var raw []byte
 if err=tx.QueryRow(ctx,"SELECT photo.read_v29_negative_adopted_replacements($1,$2,$3,$4,$5,$6,$7::jsonb)",p.tenant,p.project,p.who.UserID,p.who.SessionID,p.review,p.hash,string(body)).Scan(&raw);err!=nil{return nil,err}
 if len(raw)<2||bytes.Equal(bytes.TrimSpace(raw),[]byte("null")){return nil,v2input.ErrExpired}
 out,err:=decodeNegativeAdoptedReplacementNodes(raw,nodes);if err!=nil{return nil,err}
 if err=tx.Commit(ctx);err!=nil{return nil,err};return out,nil
}

// LoadAdoptedReplacementParents discovers rebuilt candidates through independent
// membership/adoption authority, then reads all original-version WorkingImage
// and array bytes. Retained parents are loaded only for the resulting frontier.
// Status/receipt presence alone never becomes a completion proof.
func(p PreparedNegativeReplacementInputs)LoadAdoptedReplacementParents(ctx context.Context,db NegativeTransactions,objects v2input.SessionAnalysisObjectReader)([]VerifiedAdoptedParentArrays,[]VerifiedAdoptedParentArrays,error){
 if err:=ctx.Err();err!=nil{return nil,nil,err};if db==nil||objects==nil{return nil,nil,v2input.ErrInvalid}
 checked,_,inventory,err:=p.checked(ctx);if err!=nil{return nil,nil,err}
 topology,err:=checked.Topology();if err!=nil{return nil,nil,err}
 nodes:=make([]negativeReplacementDiscoveryNode,0,len(topology.Nodes))
 for _,node:=range topology.Nodes{nodes=append(nodes,negativeReplacementDiscoveryNode{node.OriginalTask,node.Task,node.Run})}
 sort.Slice(nodes,func(i,j int)bool{return nodes[i].Original.String()<nodes[j].Original.String()})
 candidates,err:=checked.readAdoptedReplacementNodes(ctx,db,nodes);if err!=nil{return nil,nil,err}
 completed:=make([]uuid.UUID,0,len(candidates));for _,node:=range candidates{completed=append(completed,node.Task)}
 ready,err:=checked.ReadySteps(ctx,completed);if err!=nil{return nil,nil,err}
 load:=func(task,run uuid.UUID)(VerifiedAdoptedParentArrays,error){
  parent,err:=LoadAdoptedOpticalParent(ctx,db,objects,p.bucket,p.who,p.tenant,p.project,task,run);if err!=nil{return VerifiedAdoptedParentArrays{},err}
  return LoadAdoptedParentArrays(ctx,db,objects,p.bucket,p.who,parent)
 }
 rebuilt:=make([]VerifiedAdoptedParentArrays,0,len(candidates))
 for _,node:=range candidates{proof,err:=load(node.Task,node.Run);if err!=nil{return nil,nil,err};rebuilt=append(rebuilt,proof)}
 runs:=make(map[uuid.UUID]uuid.UUID,len(inventory));for _,task:=range inventory{runs[task.TaskID]=task.RunID}
 needed:=map[uuid.UUID]bool{};for _,step:=range ready{for _,parent:=range step.node.Parents{if parent.Kind=="retained"{needed[parent.Task]=true}}}
 ids:=make([]uuid.UUID,0,len(needed));for task:=range needed{ids=append(ids,task)}
 sort.Slice(ids,func(i,j int)bool{return ids[i].String()<ids[j].String()})
 retained:=make([]VerifiedAdoptedParentArrays,0,len(ids))
 for _,task:=range ids{run:=runs[task];if run==uuid.Nil{return nil,nil,v2input.ErrInvalid};proof,err:=load(task,run);if err!=nil{return nil,nil,err};retained=append(retained,proof)}
 if _,err=checked.ReadyParentSources(ctx,rebuilt,retained);err!=nil{return nil,nil,err}
 return rebuilt,retained,ctx.Err()
}
