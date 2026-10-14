package interchange
import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)
// Save persists immutable pre-adoption evidence in the caller-owned transaction.
// A false SQL receipt is stale authority; no completion state is implied.
func(p PreparedExportAdoption)Save(ctx context.Context,tx ProofTransaction,config SQLExportOutputCatalog)error{
 return p.persist(ctx,tx,config,false)
}
func(p PreparedExportAdoption)Adopt(ctx context.Context,tx ProofTransaction,config SQLExportOutputCatalog)error{
 return p.persist(ctx,tx,config,true)
}
func(p PreparedExportAdoption)persist(ctx context.Context,tx ProofTransaction,config SQLExportOutputCatalog,adopt bool)error{
 if err:=ctx.Err();err!=nil{return err}
 if tx==nil||len(p.body)<2||len(p.body)>16<<20||p.sha!=rawSHA(p.body)||!validImportRef(p.root)||!validImportRef(p.bundle){return v2input.ErrInvalid}
 intent,err:=FreezeCreateIntent(config.Tenant,config.Who,config.Request,config.Key);if err!=nil{return err}
 if _,_,err:=DecodeFrozenExportDocuments(ctx,intent,config.Frozen.Body());err!=nil{return err}
 normalized,err:=canonical.Bytes(p.body);if err!=nil||!bytes.Equal(normalized,p.body){return v2input.ErrInvalid}
 var evidence exportAdoptionEvidence
 decoder:=json.NewDecoder(bytes.NewReader(p.body));decoder.DisallowUnknownFields();if err:=decoder.Decode(&evidence);err!=nil{return err}
 encoded,err:=canonical.Marshal(evidence);if err!=nil||!bytes.Equal(encoded,p.body)||evidence.Schema!=1||evidence.Format!=intent.Request.Format||evidence.Tenant!=config.Tenant||evidence.Actor!=config.Who.UserID||evidence.Identity!=intent.Identity||evidence.RequestSHA!=intent.RequestSHA||evidence.FrozenSHA!=rawSHA(config.Frozen.Body())||evidence.Root!=p.root||evidence.Bundle!=p.bundle||evidence.Bucket!=config.Bucket{return v2input.ErrInvalid}
 var saved bool
 query:=`SELECT photo.save_v30_interchange_export_evidence($1,$2,$3,$4,$5,$6,$7)`
 if adopt{query=`SELECT photo.adopt_v30_interchange_export($1,$2,$3,$4,$5,$6,$7)`}
 if err:=tx.QueryRow(ctx,query,config.Tenant,config.Who.UserID,config.Who.SessionID,config.Key,intent.RequestSHA,string(p.body),p.sha).Scan(&saved);err!=nil{return err}
 if !saved{return domain.NewError("INTERCHANGE_RESULT_STALE",409)}
 return ctx.Err()
}
func(p ExportResultPreparation)PrepareAndSave(ctx context.Context,root v2input.Ref)(PreparedExportAdoption,error){
 evidence,err:=p.Prepare(ctx,root);if err!=nil{return PreparedExportAdoption{},err}
 err=tenant.WithTenant(ctx,p.Catalog.DB,p.Catalog.Who,p.Catalog.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{return evidence.Save(ctx,tx,p.Catalog)})
 if err!=nil{return PreparedExportAdoption{},err};return evidence,nil
}

// PrepareAndAdopt publishes links/inbox/events atomically after all object IO.
// The scheduler finalizer still owns the terminal run transition/quota release.
func(p ExportResultPreparation)PrepareAndAdopt(ctx context.Context,root v2input.Ref)(PreparedExportAdoption,error){
 evidence,err:=p.Prepare(ctx,root);if err!=nil{return PreparedExportAdoption{},err}
 err=tenant.WithTenant(ctx,p.Catalog.DB,p.Catalog.Who,p.Catalog.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{return evidence.Adopt(ctx,tx,p.Catalog)})
 if err!=nil{return PreparedExportAdoption{},err};return evidence,nil
}
