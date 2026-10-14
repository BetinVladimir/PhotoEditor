package delivery

import (
 "context"
 "encoding/json"
 "fmt"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/v2input"
)

type SignerDelegationScope struct {
 TenantID uuid.UUID `json:"tenant_id"`
 ActorID uuid.UUID `json:"actor_id"`
 ProjectID uuid.UUID `json:"project_id"`
 ParentRunID uuid.UUID `json:"parent_run_id"`
 PackageID uuid.UUID `json:"package_id"`
 Ordinal int `json:"ordinal"`
 PresetID uuid.UUID `json:"preset_id"`
 ExportID uuid.UUID `json:"export_id"`
 ExportArtifactID uuid.UUID `json:"export_artifact_id"`
 ExportSHA string `json:"export_sha"`
 PolicyID uuid.UUID `json:"policy_id"`
 PolicySHA string `json:"policy_sha"`
 IdempotencyKey string `json:"idempotency_key"`
 WorkflowID uuid.UUID `json:"workflow_id"`
 TaskID uuid.UUID `json:"task_id"`
}

func ValidateSignerDelegationScope(lease ExpansionLease,slot Slot,
 scope SignerDelegationScope)error{
 if !lease.Valid()||slot.State!="ready"||slot.ExportID==nil||slot.Export==nil||
  scope.TenantID!=lease.Tenant||scope.ParentRunID!=lease.Run||
  scope.ActorID==uuid.Nil||scope.ProjectID==uuid.Nil||scope.PackageID!=lease.Package||
  scope.Ordinal!=slot.Ordinal||scope.PresetID!=slot.PresetID||
  scope.ExportID!=*slot.ExportID||scope.ExportArtifactID!=slot.Export.ArtifactID||
  scope.ExportSHA!=slot.Export.SHA256||scope.PolicyID==uuid.Nil||
  !shaPattern.MatchString(scope.PolicySHA){return v2input.ErrInvalid}
 key:=uuid.NewSHA1(lease.Run,[]byte(fmt.Sprintf("%d:%s",slot.Ordinal,slot.PresetID))).String()
 workflow:=uuid.NewSHA1(lease.Tenant,[]byte("provenance:"+scope.ActorID.String()+":"+key))
 if scope.IdempotencyKey!=key||scope.WorkflowID!=workflow||
  scope.TaskID!=uuid.NewSHA1(workflow,[]byte("provenance.sign:0")){
  return v2input.ErrInvalid
 }
 return nil
}

// ReadSignerDelegationScope is metadata authorization only. The V31 service
// must still perform its full qualified admission under this exact scope.
func ReadSignerDelegationScope(ctx context.Context,db *pgxpool.Pool,
 lease ExpansionLease,slot Slot)(SignerDelegationScope,error){
 var scope SignerDelegationScope
 if err:=ctx.Err();err!=nil{return scope,err}
 if db==nil||!lease.Valid()||slot.Ordinal<0||slot.Ordinal>4999||
  slot.PresetID==uuid.Nil||slot.State!="ready"{return scope,v2input.ErrInvalid}
 var raw []byte
 if err:=db.QueryRow(ctx,`SELECT photo.v30_delivery_signer_delegation_scope($1,$2,$3,$4)`,
  lease.Expansion,lease.Token,slot.Ordinal,slot.PresetID).Scan(&raw);err!=nil{return scope,err}
 if len(raw)<2||len(raw)>4096||json.Unmarshal(raw,&scope)!=nil{
  return SignerDelegationScope{},v2input.ErrInvalid
 }
 if err:=ValidateSignerDelegationScope(lease,slot,scope);err!=nil{return SignerDelegationScope{},err}
 return scope,nil
}
