package interchange
import("context";"encoding/json";"strconv";"github.com/google/uuid";"photoeditor/backend/internal/artifactjson";"photoeditor/backend/internal/canonical";"photoeditor/backend/internal/domain";"photoeditor/backend/internal/v2input")
type ResolvedImportApproval struct{Entry uuid.UUID `json:"entry_id"`;StreamRevision string `json:"stream_revision"`;Artifact uuid.UUID `json:"artifact_id"`;SHA string `json:"sha256"`;StyleRegistration *ImportStyleApprovalReceipt `json:"style_registration,omitempty"`}
// Resolved receipts are produced by a trusted FrozenRecipe preparation service;
// SQL rechecks upload provenance, source, stream/document revision and approval.
func(i FrozenReviewIntent)CommitDecisions(ctx context.Context,tx ProofTransaction,selection artifactjson.Ref,batch PreparedImportDecisionBatch,resolved []ResolvedImportApproval)(json.RawMessage,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if tx==nil||i.action!="decide"||batch.run==uuid.Nil||batch.project==uuid.Nil||selection.ID==uuid.Nil||selection.SHA256!=i.documentSHA||len(resolved)!=batch.approvals{return nil,domain.NewError("VALIDATION_FAILED",422)}
 var selected []SelectedImportDecision;if json.Unmarshal(batch.raw,&selected)!=nil{return nil,domain.NewError("VALIDATION_FAILED",422)}
 approvals:=map[uuid.UUID]bool{};for _,item:=range selected{if item.Decision=="approve"{approvals[item.Target.Entry]=true}}
 seen:=map[uuid.UUID]bool{}
 var registrationBytes int64
 for _,receipt:=range resolved{if receipt.StyleRegistration==nil{return nil,v2input.ErrInvalid};if receipt.StyleRegistration!=nil{if err:=validateImportStyleApprovalReceipt(i.workflow,receipt.Entry,*receipt.StyleRegistration);err!=nil{return nil,err};registrationBytes+=int64(len(receipt.StyleRegistration.Parsed)+len(receipt.StyleRegistration.Config)+len(receipt.StyleRegistration.Recipe));if registrationBytes>128<<20{return nil,v2input.ErrInvalid}}}
 for _,receipt:=range resolved{revision,err:=strconv.ParseInt(receipt.StreamRevision,10,64);if !approvals[receipt.Entry]||seen[receipt.Entry]||err!=nil||revision<1||revision>=9007199254740991||strconv.FormatInt(revision,10)!=receipt.StreamRevision||!validImportRef(v2input.Ref{ArtifactID:receipt.Artifact,SHA256:receipt.SHA}){return nil,domain.NewError("VALIDATION_FAILED",422)};seen[receipt.Entry]=true}
 if resolved==nil{resolved=[]ResolvedImportApproval{}}
 body,err:=canonical.Marshal(resolved);if err!=nil{return nil,err};if len(body)>128<<20{return nil,v2input.ErrInvalid};var raw []byte
 err=tx.QueryRow(ctx,`SELECT photo.commit_v30_interchange_decisions($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb)`,i.tenant,i.workflow,i.actor.UserID,i.actor.SessionID,i.key,i.revision,string(i.request),selection.ID,string(i.document),string(body)).Scan(&raw);if err!=nil{return nil,err}
 var result struct{Workflow uuid.UUID `json:"workflow_id"`;Revision string `json:"revision"`;Remaining int `json:"remaining"`;Child json.RawMessage `json:"child"`;Batch uuid.UUID `json:"batch_id"`}
 if err:=strictReviewDecode(raw,&result);err!=nil{return nil,err}
 if result.Workflow!=i.workflow||result.Batch!=i.owner||result.Revision!=strconv.FormatInt(i.revision+1,10)||result.Remaining<0||result.Remaining>5000||string(result.Child)!="null"{return nil,domain.NewError("INTERCHANGE_DECISION_INVALID_RECEIPT",503)}
 return append(json.RawMessage(nil),raw...),nil
}
