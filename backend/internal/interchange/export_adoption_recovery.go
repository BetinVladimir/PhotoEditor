package interchange
import (
 "bytes"
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)
type ExportAdoptionRecovery struct{
 Schema int `json:"schema_version"`;Workflow uuid.UUID `json:"workflow_id"`;Run uuid.UUID `json:"run_id"`;Task uuid.UUID `json:"task_id"`;Result uuid.UUID `json:"result_id"`
 Status string `json:"status"`;Root v2input.Ref `json:"root"`;Report v2input.Ref `json:"report"`;Bundle v2input.Ref `json:"bundle"`;EvidenceSHA string `json:"evidence_sha256"`
}
func RecoverExportAdoption(ctx context.Context,tx ProofTransaction,who auth.Identity,tid uuid.UUID,request CreateRequest,key string)(ExportAdoptionRecovery,bool,error){
 var zero ExportAdoptionRecovery
 if err:=ctx.Err();err!=nil{return zero,false,err}
 if tx==nil{return zero,false,v2input.ErrInvalid}
 intent,err:=FreezeCreateIntent(tid,who,request,key);if err!=nil{return zero,false,err};if intent.Request.Direction!="export"{return zero,false,v2input.ErrInvalid}
 var raw []byte
 if err:=tx.QueryRow(ctx,`SELECT photo.recover_v30_interchange_export($1,$2,$3,$4,$5)`,tid,who.UserID,who.SessionID,key,intent.RequestSHA).Scan(&raw);err!=nil{return zero,false,err}
 if err:=ctx.Err();err!=nil{return zero,false,err}
 if len(raw)==0||bytes.Equal(bytes.TrimSpace(raw),[]byte("null")){return zero,false,nil}
 if len(raw)>65536{return zero,false,v2input.ErrInvalid}
 body,err:=canonical.Bytes(raw);if err!=nil{return zero,false,err}
 var recovered ExportAdoptionRecovery;if err:=decodeImportDocument(body,&recovered);err!=nil{return zero,false,err}
 if recovered.Schema!=1||recovered.Workflow!=intent.Identity.Workflow||recovered.Run!=intent.Identity.Run||recovered.Task!=intent.Identity.Task||recovered.Result==uuid.Nil||(recovered.Status!="adopted"&&recovered.Status!="completed")||!validImportRef(recovered.Root)||!validImportRef(recovered.Report)||!validImportRef(recovered.Bundle)||!validImportRef(v2input.Ref{ArtifactID:recovered.Result,SHA256:recovered.EvidenceSHA})||recovered.Root.ArtifactID==recovered.Report.ArtifactID||recovered.Root.ArtifactID==recovered.Bundle.ArtifactID||recovered.Report.ArtifactID==recovered.Bundle.ArtifactID{return zero,false,v2input.ErrInvalid}
 return recovered,true,nil
}
func(s Service)RecoverExportAdoption(ctx context.Context,who auth.Identity,tid uuid.UUID,request CreateRequest,key string)(ExportAdoptionRecovery,bool,error){
 if s.DB==nil{return ExportAdoptionRecovery{},false,domain.NewError("INTERCHANGE_UNAVAILABLE",503)}
 var recovery ExportAdoptionRecovery;var found bool
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{var err error;recovery,found,err=RecoverExportAdoption(ctx,tx,who,tid,request,key);return err})
 if err!=nil{return ExportAdoptionRecovery{},false,err};return recovery,found,nil
}
