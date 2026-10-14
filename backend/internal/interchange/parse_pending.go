package interchange

import (
 "bytes"
 "encoding/json"
 "io"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
)

// Private recovery record. Never return this object as a public creation ticket.
// Original limits are pinned so a restart cannot silently change task resources.
type ParsePending struct {
 QueueClass string `json:"queue_class,omitempty"`
 SchemaVersion int `json:"schema_version"`
 Tenant uuid.UUID `json:"tenant_id"`
 Actor uuid.UUID `json:"actor_id"`
 RequestSHA string `json:"request_sha"`
 Request CreateRequest `json:"request"`
 Evidence BundleEvidence `json:"evidence"`
 Limits ParseLimits `json:"limits"`
}
func EncodeParsePending(tenant uuid.UUID,who auth.Identity,request CreateRequest,key string,evidence BundleEvidence,limits ParseLimits)([]byte,error){
 intent,err:=FreezeCreateIntent(tenant,who,request,key);if err!=nil{return nil,err}
 if _,err:=CompileParsePlan(tenant,who,request,key,evidence,limits);err!=nil{return nil,err}
 return canonical.Marshal(ParsePending{QueueClass:"batch",SchemaVersion:1,Tenant:tenant,Actor:who.UserID,RequestSHA:intent.RequestSHA,Request:intent.Request,Evidence:evidence,Limits:limits})
}
func DecodeParsePending(tenant uuid.UUID,who auth.Identity,request CreateRequest,key string,raw []byte)(ParsePlan,error){
 invalid:=func()(ParsePlan,error){return ParsePlan{},domain.NewError("INTERCHANGE_PENDING_INVALID",409)}
 intent,err:=FreezeCreateIntent(tenant,who,request,key);if err!=nil{return ParsePlan{},err}
 if len(raw)<2||len(raw)>65536{return invalid()}
 normalized,err:=canonical.Bytes(raw);if err!=nil{return invalid()}
 var record ParsePending
 decoder:=json.NewDecoder(bytes.NewReader(normalized));decoder.DisallowUnknownFields()
 if decoder.Decode(&record)!=nil||decoder.Decode(new(any))!=io.EOF{return invalid()}
 if record.SchemaVersion!=1||(record.QueueClass!=""&&record.QueueClass!="batch")||record.Tenant!=tenant||record.Actor!=who.UserID||record.RequestSHA!=intent.RequestSHA{return invalid()}
 recordSHA,err:=canonical.Hash(record.Request);if err!=nil||recordSHA!=intent.RequestSHA{return invalid()}
 // Exact canonical reencoding also rejects missing/null required fields, whose
 // Go zero values must not silently become pinned recovery evidence.
 encoded,err:=canonical.Marshal(record);if err!=nil||!bytes.Equal(encoded,normalized){return invalid()}
 plan,err:=CompileParsePlan(tenant,who,request,key,record.Evidence,record.Limits)
 if err!=nil{return ParsePlan{},err}
 plan.queueClass=record.QueueClass
 return plan,nil
}
