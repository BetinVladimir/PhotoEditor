package v2sessions
import("context";"encoding/json";"errors";"strconv";"time";"github.com/google/uuid";"github.com/jackc/pgx/v5";"photoeditor/backend/internal/auth";"photoeditor/backend/internal/canonical";"photoeditor/backend/internal/domain";"photoeditor/backend/internal/project";"photoeditor/backend/internal/tenant")
type CaptureCapabilities struct {
 SchemaVersion int `json:"schema_version"`
 TenantID uuid.UUID `json:"tenant_id"`
 ProjectID uuid.UUID `json:"project_id"`
 TemplateID uuid.UUID `json:"template_id"`
 TemplateRevision string `json:"template_revision"`
 PolicySHA string `json:"policy_sha"`
 Available bool `json:"available"`
 Reason *string `json:"reason"`
 ExpiresAt time.Time `json:"expires_at"`
}
// CaptureCapabilities reuses both actual calibrated selectors from Create.
// It reads a current template; no receipt, session, task or capacity is created.
func(s Service) CaptureCapabilities(ctx context.Context,who auth.Identity,tid,template uuid.UUID,revision string)(out CaptureCapabilities,err error){
 expected,e:=strconv.ParseInt(revision,10,64);if e!=nil||expected<1||strconv.FormatInt(expected,10)!=revision||tid==uuid.Nil||template==uuid.Nil{return out,domain.NewError("VALIDATION_FAILED",422)}
 ctx,cancel:=context.WithTimeout(ctx,8*time.Second);defer cancel()
 err=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
  var projectID uuid.UUID;var actual int64;var policyRaw,captureRaw json.RawMessage
  e:=tx.QueryRow(ctx,`SELECT project_id,revision,policy,capture_policy FROM photo.shoot_sessions WHERE tenant_id=$1 AND id=$2`,tid,template).Scan(&projectID,&actual,&policyRaw,&captureRaw)
  if errors.Is(e,pgx.ErrNoRows){return domain.NewError("NOT_FOUND",404)};if e!=nil{return e}
  if _,e=project.Authorize(ctx,tx,tid,projectID,who.UserID,role,domain.Editor,false);e!=nil{return e};if actual!=expected{return domain.NewError("REVISION_CONFLICT",409)}
  var policy SessionPolicy;if json.Unmarshal(policyRaw,&policy)!=nil||!policyValid(policy){return domain.NewError("VALIDATION_FAILED",422)}
  capture,e:=parseCapture(captureRaw);if e!=nil{return e};if capture==nil{return domain.NewError("VALIDATION_FAILED",422)}
  hash,e:=canonical.Hash(struct{Policy json.RawMessage `json:"policy"`;Capture json.RawMessage `json:"capture_policy"`}{policyRaw,captureRaw});if e!=nil{return e}
  out=CaptureCapabilities{SchemaVersion:1,TenantID:tid,ProjectID:projectID,TemplateID:template,TemplateRevision:revision,PolicySHA:hash}
  locks,e:=qualifiedLock(ctx,tx,tid,projectID);if e==nil{e=s.verifyCalibration(ctx,tx,tid,policy.Calibration,locks.ModelSHA,locks.DatasetSHA)};if e==nil{e=s.captureReady(ctx,tx,tid,projectID,capture)}
  if e!=nil{code:=domain.AsError(e).Code;switch code{case "SESSION_UNAVAILABLE","CAPTURE_UNAVAILABLE","CALIBRATION_UNAVAILABLE":out.Reason=&code;default:return e}}else{out.Available=true}
  // External calibration IO can outlive tenant demotion/project withdrawal.
  // Project authorization must use the freshly resolved tenant role.
  currentRole,e:=tenant.AuthorizeInTransaction(ctx,tx,who,tid,domain.Editor);if e!=nil{return e};if _,e=project.Authorize(ctx,tx,tid,projectID,who.UserID,currentRole,domain.Editor,false);e!=nil{return e};if e=ctx.Err();e!=nil{return e}
  // Repeat revision after external calibration IO before emitting an advisory.
  var now time.Time;e=tx.QueryRow(ctx,`SELECT clock_timestamp() FROM photo.shoot_sessions WHERE tenant_id=$1 AND id=$2 AND revision=$3`,tid,template,expected).Scan(&now);if errors.Is(e,pgx.ErrNoRows){return domain.NewError("REVISION_CONFLICT",409)};if e!=nil{return e};out.ExpiresAt=now.Add(15*time.Second);return nil
 });return out,err
}
