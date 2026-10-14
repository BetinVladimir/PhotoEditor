package interchange
import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// FreezeImportExecutionDocuments adds the mandatory frozen evidence to the
// executable Params. The older four-field helper only assembles storage bytes;
// it is not an executable import admission contract.
func FreezeImportExecutionDocuments(ctx context.Context,identity ImportDocumentIdentity,parsedRef v2input.Ref,planRaw,parsedRaw,evidenceRaw []byte)(ImportPreparedDocuments,error){
 if err:=ctx.Err();err!=nil{return ImportPreparedDocuments{},err}
 documents,err:=FreezeImportDocuments(identity,parsedRef,planRaw);if err!=nil{return ImportPreparedDocuments{},err}
 var plan importPlanDocument;if err:=decodeImportDocument(planRaw,&plan);err!=nil{return ImportPreparedDocuments{},err}
 validator,err:=NewImportScopedDocumentValidator(plan.Project,plan.Snapshot,planRaw,parsedRaw);if err!=nil{return ImportPreparedDocuments{},err}
 ref:=v2input.Ref{ArtifactID:uuid.NewSHA1(documents.TaskID(),[]byte("interchange:evidence:1")),SHA256:rawSHA(evidenceRaw)}
 if _,err:=validator.ImportDocumentChildren(ctx,v2input.Entry{Ref:ref,Contract:"FrozenInterchangeImportEvidence"},evidenceRaw);err!=nil{return ImportPreparedDocuments{},err}
 var evidence importEvidenceDocument;if err:=decodeImportDocument(evidenceRaw,&evidence);err!=nil{return ImportPreparedDocuments{},err}
 if evidence.Workflow!=identity.Workflow||evidence.Parsed!=parsedRef{return ImportPreparedDocuments{},v2input.ErrInvalid}
 document:=ImportDocument{Purpose:"v30_interchange_import_evidence",Ref:ref,Body:append([]byte(nil),evidenceRaw...)}
 documents.evidence=&document
 params:=importParamsDocument{Schema:1,Parsed:parsedRef,Plan:documents.plan.Ref,Evidence:&ref,Report:8<<20}
 raw,err:=canonical.Marshal(params);if err!=nil{return ImportPreparedDocuments{},err}
 documents.params.Body=raw;documents.params.Ref.SHA256=rawSHA(raw)
 return documents,ctx.Err()
}
