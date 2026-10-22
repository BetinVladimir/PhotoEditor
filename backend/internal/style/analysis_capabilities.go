package style

import (
 "context"
 "errors"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
)

type analysisRuntimeSelection struct { FitLock, FitBackend, AggregateLock string }
type analysisRuntimeQuery interface { QueryRow(context.Context,string,...any) pgx.Row }
// The advisory and fresh admission use the same combined selector. This does not
// reserve workers; admission must repeat it inside the idempotency callback.
func selectAnalysisRuntimes(ctx context.Context,tx analysisRuntimeQuery,tid uuid.UUID,geometry bool)(out analysisRuntimeSelection,err error){
 err=tx.QueryRow(ctx,`SELECT fit_lock_id,fit_backend,aggregate_lock_id FROM photo.select_style_analysis_runtimes($1,$2)`,tid,geometry).Scan(&out.FitLock,&out.FitBackend,&out.AggregateLock)
 if errors.Is(err,pgx.ErrNoRows){return out,domain.NewError("STYLE_FIT_CAPABILITY_UNAVAILABLE",409)};if err!=nil{return out,err}
 if out.FitLock==""||out.FitBackend==""{return out,domain.NewError("STYLE_FIT_CAPABILITY_UNAVAILABLE",409)}
 if out.AggregateLock==""{return out,domain.NewError("STYLE_AGGREGATE_CAPABILITY_UNAVAILABLE",409)};return out,nil
}
type AnalysisCapabilities struct {
 SchemaVersion int `json:"schema_version"`
 TenantID uuid.UUID `json:"tenant_id"`
 ProjectID uuid.UUID `json:"project_id"`
 StyleID uuid.UUID `json:"style_id"`
 GeometryEnabled bool `json:"geometry_enabled"`
 Available bool `json:"available"`
 Reason *string `json:"reason"`
 Backends []string `json:"backends"`
 ExpiresAt time.Time `json:"expires_at"`
}
func(s Service) AnalysisCapabilities(ctx context.Context,who auth.Identity,tid,styleID uuid.UUID,geometry bool)(out AnalysisCapabilities,err error){
 if tid==uuid.Nil||styleID==uuid.Nil{return out,domain.NewError("VALIDATION_FAILED",422)}
 ctx,cancel:=context.WithTimeout(ctx,5*time.Second);defer cancel()
 err=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
  record,e:=authorizeStyle(ctx,tx,who,tid,styleID,role,domain.Editor,false);if e!=nil{return e}
  var now time.Time;if e=tx.QueryRow(ctx,`SELECT clock_timestamp()`).Scan(&now);e!=nil{return e}
  out=AnalysisCapabilities{SchemaVersion:1,TenantID:tid,StyleID:styleID,ProjectID:record.ProjectID,GeometryEnabled:geometry,Backends:[]string{},ExpiresAt:now.Add(15*time.Second)}
  selected,e:=selectAnalysisRuntimes(ctx,tx,tid,geometry)
  if e!=nil{code:=domain.AsError(e).Code;if code!="STYLE_FIT_CAPABILITY_UNAVAILABLE"&&code!="STYLE_AGGREGATE_CAPABILITY_UNAVAILABLE"{return e};out.Reason=&code}else{out.Available=true;out.Backends=[]string{selected.FitBackend}}
  // Runtime discovery may wait on catalog changes. Even an unavailable advisory
  // carries project identity and must use current project/tenant authority.
  currentRole,e:=tenant.AuthorizeInTransaction(ctx,tx,who,tid,domain.Editor);if e!=nil{return e}
  current,e:=authorizeStyle(ctx,tx,who,tid,styleID,currentRole,domain.Editor,false);if e!=nil{return e}
  if current.ProjectID!=record.ProjectID{return domain.NewError("NOT_FOUND",404)}
  return ctx.Err()
 });return out,err
}
