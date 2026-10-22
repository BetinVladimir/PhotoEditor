package run

import (
 "context"
 "encoding/json"
 "errors"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/project"
 "photoeditor/backend/internal/tenant"
)

type ApplyCapabilities struct {
 SchemaVersion int `json:"schema_version"`
 TenantID uuid.UUID `json:"tenant_id"`
 ProjectID uuid.UUID `json:"project_id"`
 CollectionID uuid.UUID `json:"collection_id"`
 SourceRevision string `json:"source_revision"`
 StyleVersionID uuid.UUID `json:"style_version_id"`
 StyleSHA256 string `json:"style_sha256"`
 Available bool `json:"available"`
 Reason *string `json:"reason"`
 Backends []string `json:"backends"`
 ExpiresAt time.Time `json:"expires_at"`
}
// Collection Apply's effective request has no editor/job overrides. This read
// reports its exact stored style requirements without admitting or reserving work.
func(s Service) CollectionApplyCapabilities(ctx context.Context,who auth.Identity,tid,collection,version uuid.UUID,revision string)(out ApplyCapabilities,err error){
 if tid==uuid.Nil||collection==uuid.Nil||version==uuid.Nil{return out,domain.NewError("VALIDATION_FAILED",422)}
 expected,e:=parseApplyCapabilityRevision(revision);if e!=nil{return out,e}
 ctx,cancel:=context.WithTimeout(ctx,5*time.Second);defer cancel()
 err=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)(callbackErr error){
  var projectID uuid.UUID;var actual int64;var state string
  e:=tx.QueryRow(ctx,`SELECT project_id,revision,state FROM photo.collections WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`,tid,collection).Scan(&projectID,&actual,&state)
  if errors.Is(e,pgx.ErrNoRows){return domain.NewError("NOT_FOUND",404)};if e!=nil{return e}
  if _,e=project.Authorize(ctx,tx,tid,projectID,who.UserID,role,domain.Editor,false);e!=nil{return e}
  if state!="draft"&&state!="published"||actual!=expected{return domain.NewError("REVISION_CONFLICT",409)}
  var config json.RawMessage;var hash,approval string;var now time.Time
  e=tx.QueryRow(ctx,`SELECT v.config_json,v.config_hash,v.approval_status,clock_timestamp() FROM photo.style_versions v JOIN photo.styles st ON st.tenant_id=v.tenant_id AND st.id=v.style_id JOIN photo.artifacts a ON a.tenant_id=v.tenant_id AND a.id=v.export_artifact_id AND a.sha256=v.config_hash AND a.state='ready' AND a.object_version IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) WHERE v.tenant_id=$1 AND v.id=$2 AND st.project_id=$3 AND st.archived_at IS NULL`,tid,version,projectID).Scan(&config,&hash,&approval,&now)
  if errors.Is(e,pgx.ErrNoRows){return domain.NewError("NOT_FOUND",404)};if e!=nil{return e}
  computed,e:=canonical.Hash(json.RawMessage(config));if e!=nil||computed!=hash{return domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
  if approval!="accepted"{return domain.NewError("STYLE_NOT_ACCEPTED",409)}
  var needs styleExecutionNeeds;if json.Unmarshal(config,&needs)!=nil||needs.Mode!="fixed"&&needs.Mode!="adaptive"{return domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
  out=ApplyCapabilities{SchemaVersion:1,TenantID:tid,ProjectID:projectID,CollectionID:collection,SourceRevision:revision,StyleVersionID:version,StyleSHA256:hash,Backends:[]string{},ExpiresAt:now.Add(15*time.Second)}
  defer func(){
   if callbackErr!=nil{return}
   var live bool
   e=tx.QueryRow(ctx,`SELECT EXISTS(SELECT FROM photo.collections c JOIN photo.style_versions v ON v.tenant_id=c.tenant_id AND v.id=$4 AND v.config_hash=$5 AND v.approval_status='accepted' JOIN photo.styles st ON st.tenant_id=v.tenant_id AND st.id=v.style_id AND st.project_id=c.project_id AND st.archived_at IS NULL JOIN photo.artifacts a ON a.tenant_id=v.tenant_id AND a.id=v.export_artifact_id AND a.sha256=v.config_hash AND a.state='ready' AND a.object_version IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) WHERE c.tenant_id=$1 AND c.id=$2 AND c.project_id=$3 AND c.deleted_at IS NULL AND c.state IN ('draft','published') AND c.revision=$6)`,tid,collection,projectID,version,hash,expected).Scan(&live)
   if e!=nil{callbackErr=e;return};if !live{callbackErr=domain.NewError("REVISION_CONFLICT",409);return}
   currentRole,e:=tenant.AuthorizeInTransaction(ctx,tx,who,tid,domain.Editor);if e!=nil{callbackErr=e;return}
   if _,e=project.Authorize(ctx,tx,tid,projectID,who.UserID,currentRole,domain.Editor,false);e!=nil{callbackErr=e;return};callbackErr=ctx.Err()
  }()
  if !applyMaskingModelsPresent(needs){reason:="STYLE_MODEL_BINDING_MISSING";out.Reason=&reason;return nil}
  if e=validateApplyModuleDependencies(needs);e==nil{e=validateApplyRegionBindings(needs.Extensions.RegionAdjustments,nil,needs.Geometry.Enabled)}
  if e!=nil{reason:=domain.AsError(e).Code;out.Reason=&reason;return nil}
  selected,e:=selectApplyRuntimes(ctx,tx,tid,needs,false)
  if e!=nil{code:=domain.AsError(e).Code;switch code{case "STYLE_MODEL_BINDING_MISSING","PERSON_MASK_CAPABILITY_UNAVAILABLE","RESTORATION_CAPABILITY_UNAVAILABLE","CREATIVE_CAPABILITY_UNAVAILABLE":out.Reason=&code;return nil};return e}
  for _,backend:=range []string{selected.PersonBackend,selected.RestorationBackend,selected.CreativeBackend}{if backend!=""{seen:=false;for _,old:=range out.Backends{seen=seen||old==backend};if !seen{out.Backends=append(out.Backends,backend)}}}
  out.Available=true;return nil
 });return out,err
}
