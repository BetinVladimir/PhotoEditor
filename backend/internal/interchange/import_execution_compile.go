package interchange

import (
 "bytes"
 "context"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/v2input"
)

// Metadata receipts describe committed catalogue rows, never proposed writes.
type ImportMetadataReader interface {
 ReadImportMetadata(context.Context,v2input.Ref,[]byte)([]byte,error)
}
// Optional independent receipt reader: when installed, its diagnostics must
// exactly match the outcomes captured by CompileImportPlan. It cannot invent or
// replace reasons after recipe compilation.
type ImportRecipeDiagnosticsReader interface {
 ReadImportRecipeDiagnostics(context.Context,[]byte,[]byte)([]byte,error)
}

type ImportExecutionCompiler struct {
 Plan ImportPlanPreparation
 Metadata ImportMetadataReader
 Diagnostics ImportRecipeDiagnosticsReader
}

// Compile produces executable Params only after current metadata and sources
// have survived all compiler IO. It neither publishes nor admits a task; SQL
// publication/seal/adoption retain their independent current-authority checks.
func(c ImportExecutionCompiler)Compile(ctx context.Context,identity ImportDocumentIdentity,parsedRef v2input.Ref,parsedRaw []byte,snapshot string,limits ImportPlanLimits)(ImportPreparedDocuments,error){
 if err:=ctx.Err();err!=nil{return ImportPreparedDocuments{},err}
 if c.Metadata==nil||identity.Run==uuid.Nil||identity.Workflow==uuid.Nil{return ImportPreparedDocuments{},v2input.ErrInvalid}
 parsedRaw=append([]byte(nil),parsedRaw...)
 plan,err:=c.Plan.Compile(ctx,parsedRef,parsedRaw,snapshot,limits);if err!=nil{return ImportPreparedDocuments{},err}
 base,err:=plan.Documents(identity);if err!=nil{return ImportPreparedDocuments{},err}
 metadataRaw,err:=c.Metadata.ReadImportMetadata(ctx,parsedRef,append([]byte(nil),parsedRaw...));if err!=nil{return ImportPreparedDocuments{},err}
 // Own the receipt before any further port can reuse its backing buffer.
 metadataRaw=append([]byte(nil),metadataRaw...)
 var metadata importMetadataDocument
 if err:=decodeImportDocument(metadataRaw,&metadata);err!=nil{return ImportPreparedDocuments{},err}
 diagnosticsRaw,err:=plan.DiagnosticsBody();if err!=nil{return ImportPreparedDocuments{},err}
 var diagnostics []importRecipeDiagnostic
 if err:=decodeImportDocument(diagnosticsRaw,&diagnostics);err!=nil{return ImportPreparedDocuments{},err}
 if c.Diagnostics!=nil{
  raw,err:=c.Diagnostics.ReadImportRecipeDiagnostics(ctx,plan.Body(),append([]byte(nil),parsedRaw...));if err!=nil{return ImportPreparedDocuments{},err}
  if !bytes.Equal(raw,diagnosticsRaw){return ImportPreparedDocuments{},v2input.ErrInvalid}
 }
 sourceWire:=plan.sources.document
 if sourceWire.Format=="photoeditor_bundle"{sourceWire.Format=""}
 evidence:=importEvidenceDocument{Schema:1,Workflow:identity.Workflow,Project:plan.sources.document.Project,Plan:base.plan.Ref,Parsed:parsedRef,Sources:sourceWire,Metadata:metadata,Diagnostics:diagnostics}
 evidenceRaw,err:=canonical.Marshal(evidence);if err!=nil{return ImportPreparedDocuments{},err}
 documents,err:=FreezeImportExecutionDocuments(ctx,identity,parsedRef,plan.Body(),parsedRaw,evidenceRaw);if err!=nil{return ImportPreparedDocuments{},err}
 currentMetadata,err:=c.Metadata.ReadImportMetadata(ctx,parsedRef,append([]byte(nil),parsedRaw...));if err!=nil{return ImportPreparedDocuments{},err}
 if !bytes.Equal(currentMetadata,metadataRaw){return ImportPreparedDocuments{},artifactjson.ErrStale}
 currentSources,err:=c.Plan.Sources.Read(ctx,parsedRef,append([]byte(nil),parsedRaw...));if err!=nil{return ImportPreparedDocuments{},err}
 if !bytes.Equal(currentSources.body,plan.sources.body){return ImportPreparedDocuments{},artifactjson.ErrStale}
 return documents,ctx.Err()
}

type SQLImportMetadataReader struct{DB *pgxpool.Pool;Tenant,Workflow uuid.UUID}
func(s SQLImportMetadataReader)ReadImportMetadata(ctx context.Context,parsedRef v2input.Ref,parsedRaw []byte)([]byte,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if s.DB==nil||s.Tenant==uuid.Nil||s.Workflow==uuid.Nil||!validImportRef(parsedRef)||rawSHA(parsedRaw)!=parsedRef.SHA256{return nil,v2input.ErrInvalid}
 parsedRaw=append([]byte(nil),parsedRaw...)
 if _,err:=validateParsedImportDocument(ctx,parsedRaw);err!=nil{return nil,err}
 var result []byte
 err:=database.TenantTransaction(ctx,s.DB,s.Tenant,func(tx pgx.Tx)error{
  var raw []byte
  if err:=tx.QueryRow(ctx,`SELECT photo.v30_interchange_metadata_receipts($1,$2,$3)`,s.Tenant,s.Workflow,string(parsedRaw)).Scan(&raw);err!=nil{return err}
  if len(raw)==0||string(raw)=="null"{return artifactjson.ErrStale}
  if len(raw)>8<<20{return v2input.ErrInvalid}
  normalized,err:=canonical.Bytes(raw);if err!=nil{return err}
  var document importMetadataDocument
  if err:=decodeImportDocument(normalized,&document);err!=nil{return err}
  if document.Schema!=1||document.Workflow!=s.Workflow||document.Parsed!=parsedRef||document.Project==uuid.Nil||document.Collection!=uuid.NewSHA1(s.Workflow,[]byte("interchange:draft-collection:1"))||document.Entries==nil{return v2input.ErrInvalid}
  result=append([]byte(nil),normalized...);return nil
 })
 if err!=nil{return nil,err};return result,ctx.Err()
}
