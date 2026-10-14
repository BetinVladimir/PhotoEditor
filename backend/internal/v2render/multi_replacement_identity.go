package v2render
import (
 "bytes"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)
// Comparable current metadata; SQL/object authority must still be repeated.
type MultiReplacementBindingIdentity struct{
 Tenant uuid.UUID `json:"tenant_id"`
 Project uuid.UUID `json:"project_id"`
 Review uuid.UUID `json:"review_id"`
 DecisionHash string `json:"decision_hash"`
 OriginalRun uuid.UUID `json:"original_run_id"`
 OriginalTask uuid.UUID `json:"original_task_id"`
}
func decodeMultiReplacementBindingIdentity(raw []byte)(MultiReplacementBindingIdentity,error){
 var zero MultiReplacementBindingIdentity
 if len(raw)==0||bytes.Equal(bytes.TrimSpace(raw),[]byte("null")){return zero,nil}
 if len(raw)>1024{return zero,v2input.ErrInvalid}
 normal,err:=canonical.Bytes(raw);if err!=nil{return zero,v2input.ErrInvalid}
 var value MultiReplacementBindingIdentity
 if json.Unmarshal(normal,&value)!=nil{return zero,v2input.ErrInvalid}
 exact,err:=canonical.Marshal(value);if err!=nil||!bytes.Equal(exact,normal){return zero,v2input.ErrInvalid}
 if value.Tenant==uuid.Nil||value.Project==uuid.Nil||value.Review==uuid.Nil||value.OriginalRun==uuid.Nil||value.OriginalTask==uuid.Nil{return zero,v2input.ErrInvalid}
 if _,err=domain.ParseDigest(value.DecisionHash);err!=nil{return zero,v2input.ErrInvalid}
 return value,nil
}
func(i MultiReplacementBindingIdentity)validate(run,producer uuid.UUID)error{
 raw,err:=canonical.Marshal(i);if err!=nil{return v2input.ErrInvalid}
 if _,err=decodeMultiReplacementBindingIdentity(raw);err!=nil{return err}
 namespace:=uuid.NewSHA1(i.Tenant,[]byte("V29.NegativeReplacement/v2:"+i.Project.String()+":"+i.Review.String()+":"+i.DecisionHash))
 if run!=uuid.NewSHA1(namespace,[]byte("run:"+i.OriginalRun.String()+":"+i.OriginalTask.String()))||producer!=uuid.NewSHA1(namespace,[]byte("task:"+i.OriginalTask.String())){return v2input.ErrInvalid}
 return nil
}
