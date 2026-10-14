package v2optical

import (
 "bytes"
 "context"
 "encoding/json"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

// NegativeReplacementReceipt is historical admission evidence, not current
// task status or proof that the replacement branch has completed successfully.
type NegativeReplacementReceipt struct {
 // Local execution observation only; never serialized into admission evidence.
 RecoveredExisting bool `json:"-"`
 SchemaVersion int `json:"schema_version"`
 Run uuid.UUID `json:"run_id"`
 Workflow uuid.UUID `json:"workflow_id"`
 Task uuid.UUID `json:"task_id"`
 State string `json:"state"`
 Actor uuid.UUID `json:"actor_id"`
 Session uuid.UUID `json:"auth_session_id"`
 Operation string `json:"operation"`
 DescriptorSHA string `json:"descriptor_sha"`
 Qualification NegativeQualificationPin `json:"qualification"`
 Transport json.RawMessage `json:"transport"`
 ReplacementIdentitySHA string `json:"replacement_identity_sha"`
 Review uuid.UUID `json:"review_id"`
 DecisionHash string `json:"decision_hash"`
 OriginalTask uuid.UUID `json:"original_task_id"`
 OriginalRun uuid.UUID `json:"original_run_id"`
}

// expected and descriptor must come from freshly restored qualified preparation.
func parseNegativeReplacementReceipt(ctx context.Context,expected negativeReplacementDraftSnapshot,descriptor []byte,pin NegativeQualificationPin,raw []byte)(NegativeReplacementReceipt,error){
 var zero NegativeReplacementReceipt
 if err:=ctx.Err();err!=nil{return zero,err}
 if len(raw)<2||len(raw)>65536{return zero,v2input.ErrExpired}
 normal,err:=canonical.Bytes(raw);if err!=nil{return zero,v2input.ErrInvalid}
 var receipt NegativeReplacementReceipt
 if json.Unmarshal(normal,&receipt)!=nil{return zero,v2input.ErrInvalid}
 exact,err:=canonical.Marshal(receipt);if err!=nil||!bytes.Equal(normal,exact){return zero,v2input.ErrInvalid}
 digest,err:=canonical.Hash(json.RawMessage(descriptor));if err!=nil{return zero,err}
 o,i:=expected.Owner,expected.Identity
 if expected.SchemaVersion!=2||!o.valid()||receipt.SchemaVersion!=2||receipt.State!="queued"||receipt.Run!=o.RunID||receipt.Workflow!=o.WorkflowID||receipt.Task!=o.TaskID||receipt.Actor!=o.ActorID||receipt.Session!=o.AuthSessionID||receipt.Operation!="optical.negative"||receipt.DescriptorSHA!=digest||receipt.Qualification!=pin||receipt.ReplacementIdentitySHA!=expected.IdentitySHA||receipt.Review!=i.Review||receipt.DecisionHash!=i.DecisionHash||receipt.OriginalTask!=i.OriginalTask||receipt.OriginalRun!=i.OriginalRun{return zero,v2input.ErrInvalid}
 if _,err=restoreNegativePublishedTransport(ctx,receipt.Transport,func(context.Context)([]byte,error){return append([]byte(nil),descriptor...),nil});err!=nil{return zero,err}
 receipt.Transport=append(json.RawMessage(nil),receipt.Transport...)
 return receipt,nil
}

// ReadNegativeReplacementQueuedReceipt never retries admission. It requires
// the current private source package and qualification, locks them, and reads
// the historical acknowledgement without staging or publication side effects.
func(p PreparedNegativeReplacementInputs)ReadNegativeReplacementQueuedReceipt(ctx context.Context,db NegativeTransactions,bucket string,rebuilt,retained []VerifiedAdoptedParentArrays,mappings []VerifiedNegativeAffectedSources,unchanged []VerifiedNegativeUnchangedArrays,draft NegativeReplacementDraft,snapshot uuid.UUID,located LocatedNegativeQualification,locks v2qualification.DeploymentLocks,capacity NegativeResourceEstimate,published v2input.Ref)(NegativeReplacementReceipt,error){
 var zero NegativeReplacementReceipt
 if err:=ctx.Err();err!=nil{return zero,err}
 if db==nil||bucket!=p.bucket||len(bucket)<3||len(bucket)>63{return zero,v2input.ErrInvalid}
 repository:=negativeReplacementParamsRepository{db:db,bucket:bucket,prepared:p,draft:draft,snapshot:snapshot,located:located,locks:locks,capacity:capacity,rebuilt:append([]VerifiedAdoptedParentArrays(nil),rebuilt...),retained:append([]VerifiedAdoptedParentArrays(nil),retained...),mappings:append([]VerifiedNegativeAffectedSources(nil),mappings...),unchanged:append([]VerifiedNegativeUnchangedArrays(nil),unchanged...)}
 descriptor,err:=repository.descriptor(ctx,published);if err!=nil{return zero,err}
 saved,_,err:=repository.document(ctx);if err!=nil{return zero,err}
 qualified,err:=located.Qualification();if err!=nil{return zero,err}
 pin,err:=qualified.Pin();if err!=nil{return zero,err}
 scope:=artifactjson.Scope{Tenant:p.tenant,Run:saved.Owner.RunID,Owner:saved.Owner.TaskID,Purpose:"task_params"}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=db.Begin(ctx);if err!=nil{return zero,err};if tx==nil{return zero,v2input.ErrInvalid}
 defer tx.Rollback(context.WithoutCancel(ctx))
 if err=repository.authorize(ctx,tx,scope);err!=nil{return zero,err}
 if err=repository.lock(ctx,tx);err!=nil{return zero,err}
 var raw []byte
 if err=tx.QueryRow(ctx,"SELECT photo.read_v29_negative_replacement_queued_receipt($1,$2,$3,$4,$5)",p.tenant,saved.Owner.RunID,p.who.UserID,p.who.SessionID,draft.raw).Scan(&raw);err!=nil{return zero,err}
 receipt,err:=parseNegativeReplacementReceipt(ctx,saved,descriptor,pin,raw);if err!=nil{return zero,err}
 if err=ctx.Err();err!=nil{return zero,err}
 if err=tx.Commit(ctx);err!=nil{return zero,err}
 return receipt,nil
}
