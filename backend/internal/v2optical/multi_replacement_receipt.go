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

// MultiReplacementReceipt is historical admission evidence, not current
// task status or proof that the replacement branch has completed successfully.
type MultiReplacementReceipt struct {
 // Successful local admitted-state recovery, excluded from historical receipt bytes.
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
 Qualification MultiQualificationPin `json:"qualification"`
 Transport json.RawMessage `json:"transport"`
 ReplacementIdentitySHA string `json:"replacement_identity_sha"`
 Review uuid.UUID `json:"review_id"`
 DecisionHash string `json:"decision_hash"`
 OriginalTask uuid.UUID `json:"original_task_id"`
 OriginalRun uuid.UUID `json:"original_run_id"`
}

// expected and descriptor must come from freshly restored qualified preparation.
func parseMultiReplacementReceipt(ctx context.Context,expected multiReplacementDraftSnapshot,descriptor []byte,pin MultiQualificationPin,raw []byte,roots int)(MultiReplacementReceipt,error){
 var zero MultiReplacementReceipt
 if err:=ctx.Err();err!=nil{return zero,err}
 if len(raw)<2||len(raw)>65536{return zero,v2input.ErrExpired}
 normal,err:=canonical.Bytes(raw);if err!=nil{return zero,v2input.ErrInvalid}
 var receipt MultiReplacementReceipt
 if json.Unmarshal(normal,&receipt)!=nil{return zero,v2input.ErrInvalid}
 exact,err:=canonical.Marshal(receipt);if err!=nil||!bytes.Equal(normal,exact){return zero,v2input.ErrInvalid}
 digest,err:=canonical.Hash(json.RawMessage(descriptor));if err!=nil{return zero,err}
 o,i:=expected.Draft,expected.Identity
 if expected.SchemaVersion!=2||(o.Actor==uuid.Nil||o.Session==uuid.Nil||o.SnapshotID==uuid.Nil||o.Run==uuid.Nil||o.Workflow==uuid.Nil||o.Task==uuid.Nil)||receipt.SchemaVersion!=2||receipt.State!="queued"||receipt.Run!=o.Run||receipt.Workflow!=o.Workflow||receipt.Task!=o.Task||receipt.Actor!=o.Actor||receipt.Session!=o.Session||(i.Operation!="optical.panorama"&&i.Operation!="optical.focus_stack")||receipt.Operation!=i.Operation||receipt.DescriptorSHA!=digest||receipt.Qualification!=pin||receipt.ReplacementIdentitySHA!=expected.IdentitySHA||receipt.Review!=i.Review||receipt.DecisionHash!=i.DecisionHash||receipt.OriginalTask!=i.OriginalTask||receipt.OriginalRun!=i.OriginalRun{return zero,v2input.ErrInvalid}
 if _,err=restoreBoundedOpticalPublishedTransport(ctx,receipt.Transport,func(context.Context)([]byte,error){return append([]byte(nil),descriptor...),nil},roots,8);err!=nil{return zero,err}
 receipt.Transport=append(json.RawMessage(nil),receipt.Transport...)
 return receipt,nil
}

// ReadMultiReplacementQueuedReceipt never retries admission. It requires
// the current private source package and qualification, locks them, and reads
// the historical acknowledgement without staging or publication side effects.
func(p PreparedNegativeReplacementInputs)ReadMultiReplacementQueuedReceipt(ctx context.Context,db NegativeTransactions,bucket string,rebuilt,retained []VerifiedAdoptedParentArrays,mappings []VerifiedNegativeAffectedSources,unchanged []VerifiedNegativeUnchangedArrays,draft MultiReplacementDraft,snapshot uuid.UUID,located LocatedMultiQualification,locks v2qualification.DeploymentLocks,capacity MultiResourceEstimate,published v2input.Ref)(MultiReplacementReceipt,error){
 var zero MultiReplacementReceipt
 if err:=ctx.Err();err!=nil{return zero,err}
 if db==nil||bucket!=p.bucket||len(bucket)<3||len(bucket)>63{return zero,v2input.ErrInvalid}
 repository:=multiReplacementParamsRepository{db:db,bucket:bucket,prepared:p,draft:draft,snapshot:snapshot,located:located,locks:locks,capacity:capacity,rebuilt:append([]VerifiedAdoptedParentArrays(nil),rebuilt...),retained:append([]VerifiedAdoptedParentArrays(nil),retained...),mappings:append([]VerifiedNegativeAffectedSources(nil),mappings...),unchanged:append([]VerifiedNegativeUnchangedArrays(nil),unchanged...)}
 descriptor,err:=repository.descriptor(ctx,published);if err!=nil{return zero,err}
 saved,plan,err:=repository.document(ctx);if err!=nil{return zero,err}
 qualified,err:=located.Qualification();if err!=nil{return zero,err}
 pin,err:=qualified.Pin();if err!=nil{return zero,err}
 scope:=artifactjson.Scope{Tenant:p.tenant,Run:saved.Identity.Run,Owner:saved.Identity.Task,Purpose:"task_params"}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=db.Begin(ctx);if err!=nil{return zero,err};if tx==nil{return zero,v2input.ErrInvalid}
 defer tx.Rollback(context.WithoutCancel(ctx))
 if err=repository.authorize(ctx,tx,scope);err!=nil{return zero,err}
 if err=repository.lock(ctx,tx);err!=nil{return zero,err}
 var raw []byte
 if err=tx.QueryRow(ctx,"SELECT photo.read_v29_multi_replacement_queued_receipt($1,$2,$3,$4,$5)",p.tenant,saved.Identity.Run,p.who.UserID,p.who.SessionID,draft.raw).Scan(&raw);err!=nil{return zero,err}
 receipt,err:=parseMultiReplacementReceipt(ctx,saved,descriptor,pin,raw,1+len(plan.DirectInputs));if err!=nil{return zero,err}
 if err=ctx.Err();err!=nil{return zero,err}
 if err=tx.Commit(ctx);err!=nil{return zero,err}
 return receipt,nil
}
