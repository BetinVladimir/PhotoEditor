package interchange
import(
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)
type ImportReviewTarget struct{
 Ordinal int `json:"ordinal"`;Entry uuid.UUID `json:"entry_id"`;External string `json:"external_id"`;Version *uuid.UUID `json:"asset_version_id"`
 State string `json:"state"`;Stream *uuid.UUID `json:"stream_id"`;Document *uuid.UUID `json:"document_id"`;Recipe *v2input.Ref `json:"recipe"`;Fields []json.RawMessage `json:"fields"`
}
type SelectedImportDecision struct{Target ImportReviewTarget `json:"target"`;Decision string `json:"decision"`}
type PreparedImportDecisionBatch struct{run,project uuid.UUID;raw []byte;approvals int}
func(b PreparedImportDecisionBatch)Bytes()[]byte{return append([]byte(nil),b.raw...)}
func(b PreparedImportDecisionBatch)RunID()uuid.UUID{return b.run}
func(b PreparedImportDecisionBatch)ProjectID()uuid.UUID{return b.project}
func(b PreparedImportDecisionBatch)ApprovalCount()int{return b.approvals}

func(i FrozenReviewIntent)SelectDecisionBatch(ctx context.Context,tx ProofTransaction)(PreparedImportDecisionBatch,error){
 if err:=ctx.Err();err!=nil{return PreparedImportDecisionBatch{},err}
 if tx==nil||i.action!="decide"||i.workflow==uuid.Nil||i.tenant==uuid.Nil{return PreparedImportDecisionBatch{},domain.NewError("VALIDATION_FAILED",422)}
 var raw []byte
 if err:=tx.QueryRow(ctx,`SELECT photo.v30_interchange_review_context($1,$2,$3,$4,$5,$6)`,i.tenant,i.actor.UserID,i.actor.SessionID,i.workflow,i.revision,(SQLReviewDocumentRepository{Intent:i}).reportSHA()).Scan(&raw);err!=nil{return PreparedImportDecisionBatch{},err}
 return i.selectDecisionContext(ctx,raw)
}
func(i FrozenReviewIntent)selectDecisionContext(ctx context.Context,raw []byte)(PreparedImportDecisionBatch,error){
 invalid:=func()(PreparedImportDecisionBatch,error){return PreparedImportDecisionBatch{},domain.NewError("INTERCHANGE_REVIEW_STALE",409)}
 if err:=ctx.Err();err!=nil{return PreparedImportDecisionBatch{},err}
 if len(raw)<2||len(raw)>8<<20{return invalid()}
 var current struct{
  Workflow uuid.UUID `json:"workflow_id"`;Project uuid.UUID `json:"project_id"`;Run uuid.UUID `json:"run_id"`;Revision string `json:"revision"`
  Review struct{Schema int `json:"schema_version"`;Workflow uuid.UUID `json:"workflow_id"`;Project uuid.UUID `json:"project_id"`;Task uuid.UUID `json:"task_id"`;Root v2input.Ref `json:"root"`;Report v2input.Ref `json:"report"`;Targets []ImportReviewTarget `json:"targets"`} `json:"review"`
 }
 normalized,err:=canonical.Bytes(raw);if err!=nil{return invalid()};if err:=decodeImportDocument(normalized,&current);err!=nil{return invalid()}
 var document struct{Expected string `json:"expected_revision"`;SHA string `json:"report_sha"`;Decisions []ImportDecision `json:"decisions"`}
 if json.Unmarshal(i.document,&document)!=nil||i.action!="decide"||len(document.Decisions)<1||len(document.Decisions)>100{return invalid()}
 review:=current.Review
 if current.Workflow!=i.workflow||current.Run==uuid.Nil||current.Project==uuid.Nil||current.Revision!=document.Expected||review.Schema!=1||review.Workflow!=current.Workflow||review.Project!=current.Project||review.Task!=uuid.NewSHA1(i.workflow,[]byte("v2.interchange.import:0"))||!validImportRef(review.Root)||!validImportRef(review.Report)||review.Report.SHA256!=document.SHA||len(review.Targets)<1||len(review.Targets)>5000{return invalid()}
 targets:=map[uuid.UUID]ImportReviewTarget{}
 for ordinal,target:=range review.Targets{
  if err:=ctx.Err();err!=nil{return PreparedImportDecisionBatch{},err}
  if target.Ordinal!=ordinal||target.Entry==uuid.Nil||target.External==""{return invalid()};if _,exists:=targets[target.Entry];exists{return invalid()}
  switch target.State{case "proposed","approved":
   if target.Version==nil||*target.Version==uuid.Nil||target.Stream==nil||target.Document==nil||target.Recipe==nil||!validImportRef(*target.Recipe)||*target.Stream!=uuid.NewSHA1(i.workflow,[]byte("interchange:draft-stream:"+target.Entry.String()))||*target.Document!=uuid.NewSHA1(i.workflow,[]byte("interchange:draft-document:"+target.Entry.String())){return invalid()}
  case "rejected","unsupported":default:return invalid()}
  targets[target.Entry]=target
 }
 selected:=make([]SelectedImportDecision,0,len(document.Decisions));seen:=map[uuid.UUID]bool{};approvals:=0
 for _,decision:=range document.Decisions{
  target,exists:=targets[decision.EntryID]
  if !exists||target.State!="proposed"||seen[decision.EntryID]||(decision.Decision!="approve"&&decision.Decision!="reject"){return invalid()}
  seen[decision.EntryID]=true;if decision.Decision=="approve"{approvals++};selected=append(selected,SelectedImportDecision{Target:target,Decision:decision.Decision})
 }
 encoded,err:=canonical.Marshal(selected);if err!=nil{return PreparedImportDecisionBatch{},err}
 return PreparedImportDecisionBatch{run:current.Run,project:current.Project,raw:encoded,approvals:approvals},nil
}
