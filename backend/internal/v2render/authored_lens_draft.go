package v2render

import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// AuthoredLensDraftStore changes only the protected admission config: all
// real lens document publication, blocked-task staging, closure registration,
// native worker execution and independent adoption retain existing producers.
// The original-master matcher has a separate exact-parent branch in 02606.
type AuthoredLensDraftStore struct {
 Scheduler *pgxpool.Pool
 Source AuthoredInstructionSource
 Materialized AuthoredGeometryMaterialization
 AuthoredParams []byte
}
func(s AuthoredLensDraftStore)config(ctx context.Context,tenant,actor,run uuid.UUID,draft v2input.CompiledLensDraft)([]byte,error){
 if s.Scheduler==nil||tenant!=s.Source.Tenant||actor!=s.Source.Actor||s.Materialized.Operation!="optical.lens"||len(draft.Config.Lens.Nodes)!=1{return nil,v2input.ErrInvalid}
 current,found,err:=ReadAuthoredInstructionSource(ctx,s.Scheduler,tenant,s.Source.Parent,s.Source.Instruction.Ordinal)
 if err!=nil{return nil,err}
 if !found||current.Source!=s.Source.Source||current.PredecessorSHA!=s.Source.PredecessorSHA||current.ProgramSHA!=s.Source.ProgramSHA||current.LayerStackJCS!=s.Source.LayerStackJCS{return nil,v2input.ErrExpired}
 materialized,err:=MaterializeAuthoredInstruction(current,s.AuthoredParams);if err!=nil{return nil,err}
 if materialized.Operation!="optical.lens"||!bytes.Equal(materialized.Params.Body,s.Materialized.Params.Body){return nil,v2input.ErrInvalid}
 expected,err:=v2input.RunID(tenant,actor,"authored-native/"+current.Instruction.Identity.String());if err!=nil||run!=expected{return nil,v2input.ErrInvalid}
 raw,err:=canonical.Marshal(draft.Config);if err!=nil||!bytes.Equal(raw,draft.Body){return nil,v2input.ErrInvalid}
 digest,err:=canonical.Hash(draft.Config);if err!=nil||digest!=draft.SHA256{return nil,v2input.ErrInvalid}
 node:=draft.Config.Lens.Nodes[0]
 nodeParams,err:=canonical.Marshal(node.Params);if err!=nil||!bytes.Equal(nodeParams,materialized.Params.Body){return nil,v2input.ErrInvalid}
 var config map[string]json.RawMessage;if json.Unmarshal(raw,&config)!=nil{return nil,v2input.ErrInvalid}
 marker,err:=canonical.Marshal(struct{
  Parent uuid.UUID `json:"parent_run_id"`;Ordinal int `json:"ordinal"`;Instruction uuid.UUID `json:"instruction_id"`
  Program string `json:"program_sha256"`;Predecessor string `json:"predecessor_sha256"`
  Source v2input.SessionVersionBinding `json:"input_source"`;Authored v2input.Ref `json:"authored_params"`
  AuthoredJCS string `json:"authored_params_jcs"`;ParamsJCS string `json:"params_jcs"`
 }{current.Parent,current.Instruction.Ordinal,current.Instruction.Identity,current.ProgramSHA,current.PredecessorSHA,
 current.Source,materialized.Params.Authored,string(s.AuthoredParams),string(materialized.Params.Body)})
 if err!=nil{return nil,err};config["authored_phase_proposal"]=marker
 return canonical.Marshal(config)
}
func(s AuthoredLensDraftStore)Begin(ctx context.Context,tenant,actor,run uuid.UUID,policy string,draft v2input.CompiledLensDraft)error{
 body,err:=s.config(ctx,tenant,actor,run,draft);if err!=nil{return err}
 workflow,err:=v2input.WorkflowID(run);if err!=nil{return err};var accepted bool
 err=s.Scheduler.QueryRow(ctx,`SELECT photo.begin_v25_lens_draft($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
 tenant,draft.Config.ProjectID,draft.Config.SnapshotID,actor,draft.Config.Lens.AuthSessionID,run,workflow,string(body),policy,draft.OutputBytes).Scan(&accepted)
 if err!=nil{return err};if !accepted{return v2input.ErrExpired};return nil
}
func(s AuthoredLensDraftStore)Replay(ctx context.Context,tenant,actor,run uuid.UUID,policy string,draft v2input.CompiledLensDraft)(bool,error){
 body,err:=s.config(ctx,tenant,actor,run,draft);if err!=nil{return false,err}
 workflow,err:=v2input.WorkflowID(run);if err!=nil{return false,err};var accepted bool
 err=s.Scheduler.QueryRow(ctx,`SELECT photo.replay_v25_lens_draft($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
 tenant,draft.Config.ProjectID,draft.Config.SnapshotID,actor,draft.Config.Lens.AuthSessionID,run,workflow,string(body),policy).Scan(&accepted)
 return accepted,err
}
var _ v2input.LensDraftStore=AuthoredLensDraftStore{}
var _ v2input.LensDraftReplayStore=AuthoredLensDraftStore{}
