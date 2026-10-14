package interchange

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

func(s SQLImportApprovalCatalog)BuildApprovalCatalog(ctx context.Context,fallback v2draft.ArtifactCatalog)(*ImportApprovalCatalog,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if s.DB==nil||fallback==nil||s.Scope.Tenant==uuid.Nil||s.Scope.Workflow==uuid.Nil||s.Scope.Entry==uuid.Nil||s.Who.UserID==uuid.Nil||s.Who.SessionID==uuid.Nil||s.Revision<1||s.Revision>9007199254740990||!validImportRef(v2input.Ref{ArtifactID:s.Scope.Entry,SHA256:s.ReportSHA})||len(s.Parsed)<2||len(s.Parsed)>8<<20{return nil,v2input.ErrInvalid}
 parsed:=append([]byte(nil),s.Parsed...)
 if _,err:=validateParsedImportDocument(ctx,parsed);err!=nil{return nil,err}
 binding,err:=canonical.Marshal(s.Scope.Binding);if err!=nil{return nil,err}
 var raw []byte
 err=tenant.WithTenant(ctx,s.DB,s.Who,s.Scope.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  return tx.QueryRow(ctx,`SELECT photo.v30_interchange_approval_dependencies($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb)`,s.Scope.Tenant,s.Who.UserID,s.Who.SessionID,s.Scope.Workflow,s.Revision,s.ReportSHA,string(parsed),s.Scope.Entry,string(binding)).Scan(&raw)
 });if err!=nil{return nil,err}
 entries,err:=decodeImportApprovalDependencies(raw);if err!=nil{return nil,err}
 s.Parsed=parsed
 return NewImportApprovalCatalog(s.Scope.Tenant,s.Who,entries,s,fallback)
}
func decodeImportApprovalDependencies(raw []byte)([]v2input.Entry,error){
 if len(raw)<2||len(raw)>2<<20{return nil,v2input.ErrInvalid}
 // PostgreSQL jsonb text is not JCS. Normalize this server response only;
 // archived/prepared artifact bytes retain strict canonical validation.
 normalized,err:=canonical.Bytes(raw);if err!=nil{return nil,err}
 var entries []v2input.Entry
 if err:=decodeImportDocument(normalized,&entries);err!=nil{return nil,err}
 if len(entries)<1||len(entries)>4096{return nil,v2input.ErrInvalid}
 ids:=map[uuid.UUID]bool{}
 for _,entry:=range entries{
  switch entry.Contract{case "StyleV2","CorrectionPlan","Override","LayerStack","MaskRef","Params":default:return nil,v2input.ErrInvalid}
  if !validImportRef(entry.Ref)||ids[entry.Ref.ArtifactID]{return nil,v2input.ErrInvalid};ids[entry.Ref.ArtifactID]=true
 }
 return entries,nil
}
