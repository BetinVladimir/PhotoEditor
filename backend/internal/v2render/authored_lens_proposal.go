package v2render

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/project"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

// AuthoredLensProposalRuntime reuses the real qualified lens admission,
// transport publisher, blocked task writer, input registrar and graph sealer.
// Snapshot is an actual producer-created single-original snapshot: SQL checks
// it independently against the parent, never an invented intermediate master.
// The protected store changes only the immutable authored continuation marker.
type AuthoredLensProposalRuntime struct {
 Runtime v2input.LensRuntime
 PhaseObjects SQLAuthoredPhaseObjects
}

type authoredLensSelection struct {
 objects SQLAuthoredPhaseObjects
 scheduler *pgxpool.Pool
 source AuthoredInstructionSource
 who auth.Identity
 snapshot uuid.UUID
}
func(s authoredLensSelection)Load(ctx context.Context,tid,pid,snapshot,actor,session uuid.UUID)([]v2input.LensSelectedSource,error){
 fail:=func()([]v2input.LensSelectedSource,error){return nil,v2input.ErrInvalid}
 if tid!=s.source.Tenant||pid!=s.source.Project||snapshot!=s.snapshot||actor!=s.source.Actor||session!=s.source.Session||s.who.UserID!=actor||s.who.SessionID!=session{return fail()}
 original,err:=s.objects.LoadOriginalBinding(ctx,s.scheduler,s.who,tid,pid,s.source.Parent,s.source.Instruction.Ordinal)
 if err!=nil{return nil,err}
 raw,err:=s.objects.ReadJSON(ctx,s.who,tid,pid,s.source.Parent,s.source.Instruction.Ordinal,s.source.Source.Source)
 if err!=nil{return nil,err}
 var frozen struct{Source v2input.SessionVersionBinding `json:"source"`}
 if json.Unmarshal([]byte(s.source.LayerStackJCS),&frozen)!=nil{return fail()}
 var bytes int64
 err=tenant.WithTenant(ctx,s.objects.API,s.who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
  if _,err:=project.Authorize(ctx,tx,tid,pid,actor,role,domain.Editor,false);err!=nil{return err}
  return tx.QueryRow(ctx,`SELECT bytes FROM photo.v25_lens_original_source($1,$2,$3,$4,$5,$6,$7)
   WHERE artifact_id=$8 AND sha256=$9`,tid,pid,frozen.Source.AssetVersionID,frozen.Source.Source.ArtifactID,
   frozen.Source.Source.SHA256,actor,session,original.Original.ArtifactID,original.Original.SHA256).Scan(&bytes)
 })
 if err!=nil{return nil,err};if bytes<1{return fail()}
 return []v2input.LensSelectedSource{{Ordinal:0,Binding:s.source.Source,SourceJSON:raw,Original:original,OriginalBytes:bytes}},nil
}

// Admit cannot choose a replacement source, profile or strength. All come from
// the immutable current instruction and its actual authored Params object.
// policySHA/limits are operator admission configuration, still checked by the
// existing real lens compiler and SQL admission/activation predicates.
func(r AuthoredLensProposalRuntime)Admit(ctx context.Context,who auth.Identity,tid,parent,snapshot uuid.UUID,
 ordinal int,policySHA string,limits v2input.LensAdmissionLimits)(v2input.JobTicket,error){
 fail:=func()(v2input.JobTicket,error){return v2input.JobTicket{},v2input.ErrInvalid}
 if r.Runtime.API!=r.PhaseObjects.API||r.Runtime.Bucket!=r.PhaseObjects.Bucket||snapshot==uuid.Nil{return fail()}
 coordinator,err:=r.Runtime.Coordinator(ctx);if err!=nil{return v2input.JobTicket{},err}
 source,found,err:=ReadAuthoredInstructionSource(ctx,r.Runtime.Scheduler,tid,parent,ordinal)
 if err!=nil{return v2input.JobTicket{},err};if !found||source.Actor!=who.UserID||source.Session!=who.SessionID{return fail()}
 var stack struct{Layers []struct{ID uuid.UUID `json:"id"`;Params v2input.Ref `json:"params"`} `json:"layers"`}
 if json.Unmarshal([]byte(source.LayerStackJCS),&stack)!=nil||source.Instruction.GeometryLayer==nil{return fail()}
 var ref v2input.Ref;matches:=0
 for _,layer:=range stack.Layers{if layer.ID==*source.Instruction.GeometryLayer{matches++;ref=layer.Params}}
 if matches!=1{return fail()}
 authored,err:=r.PhaseObjects.ReadJSON(ctx,who,tid,source.Project,parent,ordinal,ref)
 if err!=nil{return v2input.JobTicket{},err}
 materialized,err:=MaterializeAuthoredInstruction(source,authored)
 if err!=nil{return v2input.JobTicket{},err};if materialized.Operation!="optical.lens"{return fail()}
 var params v2input.OpticalLensParams
 if decodeAuthoredParams(materialized.Params.Body,&params)!=nil{return fail()}
 coordinator.Selection=authoredLensSelection{objects:r.PhaseObjects,scheduler:r.Runtime.Scheduler,source:source,who:who,snapshot:snapshot}
 coordinator.Drafts.Store=AuthoredLensDraftStore{Scheduler:r.Runtime.Scheduler,Source:source,Materialized:materialized,AuthoredParams:authored}
 return coordinator.Admit(ctx,v2input.LensAdmissionRequest{Tenant:tid,Actor:who.UserID,Project:source.Project,
  Snapshot:snapshot,Session:who.SessionID,Key:"authored-native/"+source.Instruction.Identity.String(),PolicySHA:policySHA,
  Choices:[]v2input.LensProfileChoice{{AssetVersionID:source.Source.AssetVersionID,Strength:params.Strength,Profile:params.Profile}},Limits:limits})
}
var _ v2input.LensSelectionLoader=authoredLensSelection{}
