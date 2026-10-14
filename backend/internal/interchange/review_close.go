package interchange
import(
 "context"
 "encoding/json"
 "strconv"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/domain"
)

// CommitClose runs in the caller's pending idempotency transaction after object
// publication. SQL independently proves ready selection/current review authority.
func(i FrozenReviewIntent)CommitClose(ctx context.Context,tx ProofTransaction,selection artifactjson.Ref)(json.RawMessage,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if tx==nil||i.action!="close"||i.workflow==uuid.Nil||i.tenant==uuid.Nil||selection.ID==uuid.Nil||selection.SHA256!=i.documentSHA{return nil,domain.NewError("VALIDATION_FAILED",422)}
 var raw []byte
 err:=tx.QueryRow(ctx,`SELECT photo.commit_v30_interchange_review_close($1,$2,$3,$4,$5,$6,$7,$8,$9)`,i.tenant,i.workflow,i.actor.UserID,i.actor.SessionID,i.key,i.revision,string(i.request),selection.ID,string(i.document)).Scan(&raw)
 if err!=nil{return nil,err}
 var status struct{Run uuid.UUID `json:"run_id"`;Workflow uuid.UUID `json:"workflow_id"`;Revision string `json:"revision"`;State string `json:"state"`;Outputs []json.RawMessage `json:"outputs"`;Error *string `json:"error_code"`}
 if err:=strictReviewDecode(raw,&status);err!=nil{return nil,err}
 if status.Run==uuid.Nil||status.Workflow!=i.workflow||status.Revision!=strconv.FormatInt(i.revision+1,10)||status.State!="completed"||status.Outputs==nil||len(status.Outputs)!=0||status.Error!=nil{return nil,domain.NewError("INTERCHANGE_CLOSE_INVALID_RECEIPT",503)}
 return append(json.RawMessage(nil),raw...),nil
}
