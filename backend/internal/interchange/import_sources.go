package interchange

import (
 "context"
 "unicode/utf8"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/v2input"
)

type importSourceEntry struct {
 Ordinal int `json:"ordinal"`
 ID uuid.UUID `json:"entry_id"`
 State string `json:"state"`
 Reason *string `json:"reason"`
 Version *uuid.UUID `json:"asset_version_id"`
 Master *v2input.Ref `json:"master"`
}
type importSourcesDocument struct {
 Schema int `json:"schema_version"`
 Workflow uuid.UUID `json:"workflow_id"`
 Project uuid.UUID `json:"project_id"`
 Format string `json:"format,omitempty"`
 Parsed v2input.Ref `json:"parsed"`
 IngestRun *uuid.UUID `json:"ingest_run_id"`
 Entries []importSourceEntry `json:"entries"`
}

// ImportSourceInventory owns a snapshot of current SQL evidence. It is not an
// authorization capability: publication, materialization and adoption recheck
// current authority independently. Terminal reasons must survive plan compile.
type ImportSourceInventory struct{document importSourcesDocument;body []byte}
func(s ImportSourceInventory)Body()[]byte{return append([]byte(nil),s.body...)}

func freezeImportSources(ctx context.Context,workflow,project uuid.UUID,parsedRef v2input.Ref,parsedRaw,raw []byte)(ImportSourceInventory,error){
 if err:=ctx.Err();err!=nil{return ImportSourceInventory{},err}
 if workflow==uuid.Nil||project==uuid.Nil||!validImportRef(parsedRef)||rawSHA(parsedRaw)!=parsedRef.SHA256{return ImportSourceInventory{},v2input.ErrInvalid}
 if _,err:=validateParsedImportDocument(ctx,parsedRaw);err!=nil{return ImportSourceInventory{},err}
 var parsed importParsedDocument;var sources importSourcesDocument
 if err:=decodeImportDocument(parsedRaw,&parsed);err!=nil{return ImportSourceInventory{},err}
 if err:=decodeImportDocument(raw,&sources);err!=nil{return ImportSourceInventory{},err}
 if sources.Schema!=1||sources.Workflow!=workflow||sources.Project!=project||sources.Parsed!=parsedRef||len(sources.Entries)!=len(parsed.Manifest.Entries){return ImportSourceInventory{},v2input.ErrInvalid}
 if sources.Format==""{sources.Format="photoeditor_bundle"}
 if !supportedParseFormat(sources.Format){return ImportSourceInventory{},v2input.ErrInvalid}
 if sources.IngestRun!=nil&&*sources.IngestRun!=uuid.NewSHA1(workflow,[]byte("interchange:canonical-ingest:1")){return ImportSourceInventory{},v2input.ErrInvalid}
 needsIngest:=false
 for ordinal,entry:=range sources.Entries{
  if err:=ctx.Err();err!=nil{return ImportSourceInventory{},err}
  if entry.Ordinal!=ordinal||entry.ID!=parsed.Manifest.Entries[ordinal].ID{return ImportSourceInventory{},v2input.ErrInvalid}
  switch entry.State{
  case "adopted":
   needsIngest=true
   if entry.Reason!=nil||entry.Version==nil||*entry.Version==uuid.Nil||entry.Master==nil||!validImportRef(*entry.Master){return ImportSourceInventory{},v2input.ErrInvalid}
  case "rejected","unsupported":
   if entry.State=="rejected"{needsIngest=true}
   if entry.Version!=nil||entry.Master!=nil||entry.Reason==nil||!utf8.ValidString(*entry.Reason)||utf8.RuneCountInString(*entry.Reason)<1||utf8.RuneCountInString(*entry.Reason)>200{return ImportSourceInventory{},v2input.ErrInvalid}
  default:return ImportSourceInventory{},v2input.ErrInvalid
  }
 }
 if needsIngest!=(sources.IngestRun!=nil){return ImportSourceInventory{},v2input.ErrInvalid}
 return ImportSourceInventory{document:sources,body:append([]byte(nil),raw...)},ctx.Err()
}

type SQLImportSourceReader struct{DB *pgxpool.Pool;Tenant,Workflow,Project uuid.UUID}
func(s SQLImportSourceReader)Read(ctx context.Context,parsedRef v2input.Ref,parsedRaw []byte)(ImportSourceInventory,error){
 if err:=ctx.Err();err!=nil{return ImportSourceInventory{},err}
 if s.DB==nil||s.Tenant==uuid.Nil||s.Workflow==uuid.Nil||s.Project==uuid.Nil||!validImportRef(parsedRef)||len(parsedRaw)<2||len(parsedRaw)>8<<20||rawSHA(parsedRaw)!=parsedRef.SHA256{return ImportSourceInventory{},v2input.ErrInvalid}
 parsedRaw=append([]byte(nil),parsedRaw...)
 if _,err:=validateParsedImportDocument(ctx,parsedRaw);err!=nil{return ImportSourceInventory{},err}
 var result ImportSourceInventory
 err:=database.TenantTransaction(ctx,s.DB,s.Tenant,func(tx pgx.Tx)error{
  var raw []byte
  if err:=tx.QueryRow(ctx,`SELECT photo.v30_interchange_import_sources($1,$2,$3)`,s.Tenant,s.Workflow,string(parsedRaw)).Scan(&raw);err!=nil{return err}
  if len(raw)==0||string(raw)=="null"{return artifactjson.ErrStale}
  normalized,err:=canonical.Bytes(raw);if err!=nil{return err}
  result,err=freezeImportSources(ctx,s.Workflow,s.Project,parsedRef,parsedRaw,normalized);return err
 })
 if err!=nil{return ImportSourceInventory{},err};return result,nil
}
