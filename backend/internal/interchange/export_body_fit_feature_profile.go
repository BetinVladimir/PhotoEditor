package interchange

import (
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

func(s SQLExportDocumentCatalog)MatchExportBodyFitFeatureProfile(ctx context.Context,origin ExportBodyDependencyOrigin,parent v2input.Entry,raw []byte,fit ExportClosureNode,child v2input.Entry,expected v2qualification.Expected)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.DB==nil||(expected.Operation!="pose.detect"&&expected.Operation!="face.landmarks")||expected.Operation!=origin.Producer.Operation||expected.Version!="1"||!expected.LicenseCleared{return v2input.ErrInvalid}
 body:=append([]byte(nil),raw...)
 fit=ExportClosureNode{Entry:fit.Entry,Body:append([]byte(nil),fit.Body...)}
 policy,err:=canonical.Marshal(expected);if err!=nil{return err}
 before,err:=s.LookupExportBodyFitFeatureOrigin(ctx,parent,body,fit,child);if err!=nil{return err};if before!=origin{return v2input.ErrInvalid}
 err=tenant.WithTenant(ctx,s.DB,s.Who,s.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var allowed bool
  err:=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM photo.tasks producer
   JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id AND attempt.task_id=producer.id AND attempt.attempt=producer.attempt AND attempt.fencing_token=producer.fencing_token
   WHERE producer.tenant_id=$1 AND producer.id=$2 AND producer.attempt=$3 AND producer.fencing_token=$4
   AND producer.operation=$5::jsonb->>'Operation' AND producer.operation_version=$5::jsonb->>'Version'
   AND producer.state='succeeded' AND producer.resource_class='gpu' AND attempt.finished_at IS NOT NULL
   AND attempt.backend=$5::jsonb->>'Backend'
   AND producer.execution_profile->>'backend'=$5::jsonb->>'Backend' AND attempt.profile->>'backend'=$5::jsonb->>'Backend'
   AND producer.execution_profile->>'model_lock_sha'=$5::jsonb->>'ModelLockSHA' AND attempt.profile->>'model_lock_sha'=$5::jsonb->>'ModelLockSHA'
   AND producer.execution_profile->>'runtime_lock_sha'=$5::jsonb->>'RuntimeLockSHA' AND attempt.profile->>'runtime_lock_sha'=$5::jsonb->>'RuntimeLockSHA'
   AND producer.execution_profile->>'device_profile_sha'=$5::jsonb->>'DeviceProfileSHA' AND attempt.profile->>'device_profile_sha'=$5::jsonb->>'DeviceProfileSHA')`,s.Tenant,origin.Producer.Task,origin.Producer.Attempt,origin.Producer.Fence,policy).Scan(&allowed)
  if err!=nil{return err};if !allowed{return v2qualification.ErrUnqualified};return ctx.Err()
 });if err!=nil{return err}
 after,err:=s.LookupExportBodyFitFeatureOrigin(ctx,parent,body,fit,child);if err!=nil{return err};if after!=origin{return v2input.ErrInvalid};return ctx.Err()
}
