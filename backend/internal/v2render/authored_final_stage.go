package v2render

import (
 "bytes"
 "context"
 "encoding/json"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/v2input"
)

// SQLAuthoredFinalAdmission stages only a protected compose/2 blocked shell.
// Generic transport registration and all public graph-seal wrappers remain a
// separate transaction; a staged or published shell is not runnable evidence.
type SQLAuthoredFinalAdmission struct{Scheduler *pgxpool.Pool}
func(s SQLAuthoredFinalAdmission)Stage(ctx context.Context,tenant,parent uuid.UUID,ordinal int,plan AuthoredFinalTaskPlan)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.Scheduler==nil{return v2input.ErrInvalid}
 if err:=plan.validateFrozen();err!=nil{return err}
 inventory,found,err:=ReadAuthoredFinalInventory(ctx,s.Scheduler,tenant,parent,ordinal)
 if err!=nil{return err};if !found{return v2input.ErrExpired}
 source:=inventory.Context
 if source.Instruction.Identity!=plan.Params.Params.InstructionID||source.Parent!=plan.Params.Params.ParentRunID||source.Tenant!=plan.Params.Params.TenantID||source.Workflow!=plan.Params.Params.WorkflowID{return v2input.ErrInvalid}
 return database.InTransaction(ctx,s.Scheduler,func(tx pgx.Tx)error{
  var role string;if err:=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil{return err};if role!="photo_scheduler"{return v2input.ErrInvalid}
  var snapshot uuid.UUID
  if err:=tx.QueryRow(ctx,"SELECT input_snapshot_id FROM photo.runs WHERE tenant_id=$1 AND id=$2",tenant,parent).Scan(&snapshot);err!=nil{return err}
  if snapshot==uuid.Nil{return v2input.ErrInvalid}
  config,err:=canonical.Marshal(map[string]any{
   "project_id":source.Project,"input_snapshot_id":snapshot,
   "authored_phase":map[string]any{"parent_run_id":parent,"instruction_id":source.Instruction.Identity,"program_sha256":source.ProgramSHA,"predecessor_sha256":source.PredecessorSHA,"input_source":source.Source},
   "authored_final":map[string]any{"ordinal":ordinal,"task_id":plan.Task.ID,"params":plan.ParamsRef,"params_jcs":string(plan.Params.JSON),"input_digest":plan.Task.InputDigest,"profile":json.RawMessage(plan.Task.ExecutionProfile)},
  });if err!=nil{return err}
  var accepted bool
  if err=tx.QueryRow(ctx,`SELECT photo.begin_authored_render_final_task($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9)`,tenant,parent,ordinal,plan.Task.ID,plan.ParamsRef.ArtifactID,plan.ParamsRef.SHA256,plan.Task.InputDigest,plan.Task.ExecutionProfile,string(config)).Scan(&accepted);err!=nil{return err}
  if !accepted{return v2input.ErrExpired};return nil
 })
}

// PublishTransport invokes the existing real backend document protocol, after
// Stage has committed. The generic repository independently checks actual task
// scope at reserve AND complete and pins exact immutable object versions.
func(s SQLAuthoredFinalAdmission)PublishTransport(ctx context.Context,tenant,parent uuid.UUID,ordinal int,plan AuthoredFinalTaskPlan,bucket string,objects artifactjson.Objects)(v2input.GraphTask,error){
 empty:=v2input.GraphTask{}
 if s.Scheduler==nil||bucket==""||objects==nil{return empty,v2input.ErrInvalid}
 if err:=plan.validateFrozen();err!=nil{return empty,err}
 before,found,err:=ReadAuthoredFinalInventory(ctx,s.Scheduler,tenant,parent,ordinal)
 if err!=nil{return empty,err};if !found{return empty,v2input.ErrExpired}
 if before.Context.Instruction.Identity!=plan.Params.Params.InstructionID||before.Context.Parent!=plan.Params.Params.ParentRunID||before.Context.Tenant!=plan.Params.Params.TenantID{return empty,v2input.ErrInvalid}
 publisher:=v2input.ScopedPublisher{Tenant:tenant,Run:plan.Run,Documents:artifactjson.Publisher{Repository:artifactjson.SQLRepository{DB:s.Scheduler,Bucket:bucket},Objects:objects}}
 graph,err:=PublishAuthoredFinalTransport(ctx,plan,publisher);if err!=nil{return empty,err}
 after,present,err:=ReadAuthoredFinalInventory(ctx,s.Scheduler,tenant,parent,ordinal)
 if err!=nil{return empty,err};if !present{return empty,v2input.ErrExpired}
 a,err:=canonical.Marshal(before);if err!=nil{return empty,err};b,err:=canonical.Marshal(after);if err!=nil{return empty,err}
 if !bytes.Equal(a,b){return empty,v2input.ErrExpired}
 if err=ctx.Err();err!=nil{return empty,err};return graph,nil
}
