package v2render

import (
 "context"
 "encoding/json"

 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

// AuthoredDefectProposalPlan retains the ordinary native defect compiler. It
// grants no admission: the protected publisher/stager must reread the current
// instruction, qualification and actual source-specific proposal dependencies.
// In particular a prior proposal/review cannot become this child's authority.
type AuthoredDefectProposalPlan struct {
 Run uuid.UUID
 Workflow uuid.UUID
 Instruction uuid.UUID
 OriginalParams v2input.Ref
 Frozen v2input.FrozenSemanticDefectTask
}

func CompileAuthoredDefectProposalTask(ctx context.Context, source AuthoredInstructionSource,
 imageBody,paramsBody []byte,paramsRef v2input.Ref,entries []v2input.Entry,
 sizes []v2input.SemanticInputSize,costs v2input.SemanticQualifiedCosts,
 ram,vram,scratch,output int64)(AuthoredDefectProposalPlan,error){
 empty:=AuthoredDefectProposalPlan{}
 if err:=ctx.Err();err!=nil{return empty,err}
 if source.Tenant==uuid.Nil||source.Parent==uuid.Nil||source.Workflow!=uuid.NewSHA1(source.Parent,[]byte("workflow"))||source.Instruction.Kind!="segment"||source.Instruction.Ordinal<1||source.Instruction.Ordinal>4998||len(source.Instruction.Layers)!=1||source.Instruction.GeometryLayer!=nil||!authoredBindingValid(source.Source){return empty,v2input.ErrInvalid}
 instruction,err:=v2input.TaskID(source.Workflow,"render.authored_segment",source.Instruction.Ordinal)
 if err!=nil||instruction!=source.Instruction.Identity{return empty,v2input.ErrInvalid}
 var stack struct{Layers []struct{
  ID uuid.UUID `json:"id"`
  Operation string `json:"operation"`
  Version string `json:"operation_version"`
  Enabled bool `json:"enabled"`
  Opacity float64 `json:"opacity"`
  Params v2input.Ref `json:"params"`
 } `json:"layers"`}
 if json.Unmarshal([]byte(source.LayerStackJCS),&stack)!=nil{return empty,v2input.ErrInvalid}
 matches:=0;var original v2input.Ref
 for _,layer:=range stack.Layers{
  if layer.ID!=source.Instruction.Layers[0]{continue};matches++
  if (layer.Operation!="defects.repair"&&layer.Operation!="defects.auto_repair")||layer.Version!="1"||!layer.Enabled||layer.Opacity<=0||layer.Opacity>1||layer.Params.ArtifactID==uuid.Nil{return empty,v2input.ErrInvalid}
  original=layer.Params
 }
 if matches!=1{return empty,v2input.ErrInvalid}
 run:=uuid.NewSHA1(instruction,[]byte("AuthoredRepairProposal/v1:defects.propose"))
 workflow:=uuid.NewSHA1(run,[]byte("workflow"))
 // This native compiler validates actual WorkingImage, complete typed input
 // inventory, declared face/protection Params, resource bounds and digest.
 frozen,err:=v2input.CompileSemanticDefectTask(ctx,workflow,0,source.Source,imageBody,paramsBody,paramsRef,entries,sizes,costs,ram,vram,scratch,output)
 if err!=nil{return empty,err}
 task,err:=frozen.DraftTask();if err!=nil{return empty,err}
 expected,err:=v2input.TaskID(workflow,"defects.propose",0)
 if err!=nil||task.ID!=expected||task.Operation!="defects.propose"||task.OperationVersion!="1"||task.NodeKey!="defects.propose:0"||task.ResourceClass!="cpu"{return empty,v2input.ErrInvalid}
 return AuthoredDefectProposalPlan{Run:run,Workflow:workflow,Instruction:instruction,OriginalParams:original,Frozen:frozen},nil
}
