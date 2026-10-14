package interchange
import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)
// Operator-only configuration; report claims never choose expected locks.
type ImportStatsQualificationInstallation struct{Report v2input.Ref;Locks v2qualification.DeploymentLocks}
type sqlImportStatsRuntimePolicy struct{store SQLPreparedImportArtifactStore;objects ImportDocumentObjects;report v2input.Ref;expected []byte}
func(i ImportStatsQualificationInstallation)Build(ctx context.Context,store SQLPreparedImportArtifactStore,objects ImportDocumentObjects)(ImportLayerOperationQualifier,string,error){
 if err:=ctx.Err();err!=nil{return nil,"",err}
 if store.DB==nil||objects==nil||store.Tenant==uuid.Nil||store.Identity.Workflow==uuid.Nil||store.Bucket==""||!validImportRef(i.Report){return nil,"",v2input.ErrInvalid}
 if _,err:=validateParsedImportDocument(ctx,store.Parsed);err!=nil{return nil,"",err}
 expected,err:=i.Locks.Expected();if err!=nil{return nil,"",err}
 if expected.Operation!="color.stats"||expected.Version!="1"{return nil,"",v2input.ErrInvalid}
 frozen,err:=canonical.Marshal(expected);if err!=nil{return nil,"",err}
 store.Parsed=append([]byte(nil),store.Parsed...)
 return sqlImportStatsRuntimePolicy{store:store,objects:objects,report:i.Report,expected:frozen},expected.ModelLockSHA,ctx.Err()
}
func(q sqlImportStatsRuntimePolicy)QualifyImportedLayerOperation(ctx context.Context,operation,version string,raw []byte,_ v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 if q.store.DB==nil||q.objects==nil||operation!="color.stats"||version!="1"{return v2input.ErrInvalid}
 if _,err:=decodeImportStatsParams(raw);err!=nil{return err}
 lookup:=func()(objectstore.Object,error){
  var out objectstore.Object
  err:=database.TenantTransaction(ctx,q.store.DB,q.store.Tenant,func(tx pgx.Tx)error{
   var role string;if err:=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil{return err};if role!="photo_scheduler"{return v2input.ErrInvalid}
   rows,err:=tx.Query(ctx,`SELECT artifact_id,sha256,bucket,object_key,object_version,bytes FROM photo.v30_import_stats_qualification_object($1,$2,$3,$4,$5,$6)`,q.store.Tenant,q.store.Identity.Workflow,string(q.store.Parsed),q.expected,q.report.ArtifactID,q.report.SHA256);if err!=nil{return err};defer rows.Close()
   found:=false
   for rows.Next(){var ref v2input.Ref;var bucket string;var object objectstore.Object
    if err:=rows.Scan(&ref.ArtifactID,&ref.SHA256,&bucket,&object.Key,&object.VersionID,&object.Bytes);err!=nil{return err}
    if found||ref!=q.report||bucket!=q.store.Bucket||object.Key==""||object.VersionID==""||object.VersionID=="null"||object.Bytes<2||object.Bytes>8<<20{return v2input.ErrInvalid}
    found=true;object.SHA256=ref.SHA256;object.ContentType="application/json";out=object
   }
   if err:=rows.Err();err!=nil{return err};if !found{return v2qualification.ErrUnqualified};return ctx.Err()
  });return out,err
 }
 return qualifyImportRestorationRuntimeReport(ctx,"color.stats",q.objects,q.report,q.expected,lookup)
}

func(i ImportStatsQualificationInstallation)owned()ImportStatsQualificationInstallation{
 i.Locks.RequiredChecks=append([]string(nil),i.Locks.RequiredChecks...)
 i.Locks.CalibrationSHAs=append([]string(nil),i.Locks.CalibrationSHAs...)
 i.Locks.TestFixtureSHAs=append([]string(nil),i.Locks.TestFixtureSHAs...)
 i.Locks.EvidenceSHAs=append([]string(nil),i.Locks.EvidenceSHAs...)
 return i
}
