package interchange
import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)
// Operator-only configuration; report claims never choose expected locks.
type ImportPersonMaskQualificationInstallation struct{Report v2input.Ref;Locks v2qualification.DeploymentLocks;Consent ImportMaskQualifier}
type sqlImportPersonMaskRuntimePolicy struct{store SQLPreparedImportArtifactStore;objects ImportDocumentObjects;report v2input.Ref;expected []byte;modelLock string;consent ImportMaskQualifier}
func(i ImportPersonMaskQualificationInstallation)Build(ctx context.Context,store SQLPreparedImportArtifactStore,objects ImportDocumentObjects)(ImportMaskQualifier,string,error){
 if err:=ctx.Err();err!=nil{return nil,"",err}
 if i.Consent==nil||store.DB==nil||objects==nil||store.Tenant==uuid.Nil||store.Identity.Workflow==uuid.Nil||store.Bucket==""||!validImportRef(i.Report){return nil,"",v2input.ErrInvalid}
 if _,err:=validateParsedImportDocument(ctx,store.Parsed);err!=nil{return nil,"",err}
 expected,err:=i.Locks.Expected();if err!=nil{return nil,"",err}
 if expected.Operation!="mask.person"||expected.Version!="1"{return nil,"",v2input.ErrInvalid}
 frozen,err:=canonical.Marshal(expected);if err!=nil{return nil,"",err}
 store.Parsed=append([]byte(nil),store.Parsed...)
 return sqlImportPersonMaskRuntimePolicy{store:store,objects:objects,report:i.Report,expected:frozen,modelLock:expected.ModelLockSHA,consent:i.Consent},expected.ModelLockSHA,ctx.Err()
}
func(q sqlImportPersonMaskRuntimePolicy)QualifyImportedMask(ctx context.Context,mask review.MaskRef,binding v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 if q.store.DB==nil||q.objects==nil||q.consent==nil{return v2input.ErrInvalid}
 return qualifyImportedPersonMaskPolicy(ctx,mask,binding,q.modelLock,q.consent,func()error{
 lookup:=func()(objectstore.Object,error){
  var out objectstore.Object
  err:=database.TenantTransaction(ctx,q.store.DB,q.store.Tenant,func(tx pgx.Tx)error{
   var role string;if err:=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil{return err};if role!="photo_scheduler"{return v2input.ErrInvalid}
   rows,err:=tx.Query(ctx,`SELECT artifact_id,sha256,bucket,object_key,object_version,bytes FROM photo.v30_import_person_mask_qualification_object($1,$2,$3,$4,$5,$6)`,q.store.Tenant,q.store.Identity.Workflow,string(q.store.Parsed),q.expected,q.report.ArtifactID,q.report.SHA256);if err!=nil{return err};defer rows.Close()
   found:=false
   for rows.Next(){var ref v2input.Ref;var bucket string;var object objectstore.Object
    if err:=rows.Scan(&ref.ArtifactID,&ref.SHA256,&bucket,&object.Key,&object.VersionID,&object.Bytes);err!=nil{return err}
    if found||ref!=q.report||bucket!=q.store.Bucket||object.Key==""||object.VersionID==""||object.VersionID=="null"||object.Bytes<2||object.Bytes>8<<20{return v2input.ErrInvalid}
    found=true;object.SHA256=ref.SHA256;object.ContentType="application/json";out=object
   }
   if err:=rows.Err();err!=nil{return err};if !found{return v2qualification.ErrUnqualified};return ctx.Err()
  });return out,err
 }
 return qualifyImportRestorationRuntimeReport(ctx,"mask.person",q.objects,q.report,q.expected,lookup)
 })
}

func qualifyImportedPersonMaskPolicy(ctx context.Context,mask review.MaskRef,binding v2input.SessionVersionBinding,lock string,consent ImportMaskQualifier,runtime func()error)error{
 if err:=ctx.Err();err!=nil{return err}
 if consent==nil||runtime==nil||!validImportRefForSHA(lock)||binding.AssetVersionID==uuid.Nil||!validImportRef(binding.Source)||!validImportRefForSHA(binding.CanvasHash){return v2input.ErrInvalid}
 raw,err:=canonical.Marshal(mask);if err!=nil{return err}
 owned,err:=review.ParseMaskRef(raw);if err!=nil{return err}
 if owned.Selector!="person"||owned.ObjectID!=nil||owned.ModelLockID==nil||*owned.ModelLockID!=lock{return v2input.ErrInvalid}
 if err:=consent.QualifyImportedMask(ctx,owned,binding);err!=nil{return err}
 if err:=ctx.Err();err!=nil{return err}
 if err:=runtime();err!=nil{return err}
 if err:=ctx.Err();err!=nil{return err}
 owned,err=review.ParseMaskRef(append([]byte(nil),raw...));if err!=nil{return err}
 if err:=consent.QualifyImportedMask(ctx,owned,binding);err!=nil{return err};return ctx.Err()
}

func(i ImportPersonMaskQualificationInstallation)owned()ImportPersonMaskQualificationInstallation{
 i.Locks.RequiredChecks=append([]string(nil),i.Locks.RequiredChecks...)
 i.Locks.CalibrationSHAs=append([]string(nil),i.Locks.CalibrationSHAs...)
 i.Locks.TestFixtureSHAs=append([]string(nil),i.Locks.TestFixtureSHAs...)
 i.Locks.EvidenceSHAs=append([]string(nil),i.Locks.EvidenceSHAs...)
 return i
}
