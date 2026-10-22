package v2workflow

import (
 "bytes"
 "encoding/json"
 "io"
 "github.com/google/uuid"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
)

func validateCancelReceipt(out idempotency.Response,workflow uuid.UUID)error{
 invalid:=func()error{return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
 var fields map[string]json.RawMessage
 if json.Unmarshal(out.Body,&fields)!=nil||len(fields)!=6{return invalid()}
 for _,key:=range []string{"run_id","workflow_id","revision","state","outputs","error_code"}{if _,ok:=fields[key];!ok{return invalid()}}
 var value Status
 decoder:=json.NewDecoder(bytes.NewReader(out.Body));decoder.DisallowUnknownFields()
 if decoder.Decode(&value)!=nil||decoder.Decode(new(any))!=io.EOF||out.Status!=200||out.ResourceID==nil||*out.ResourceID!=workflow||value.WorkflowID!=workflow||value.RunID==uuid.Nil||value.Outputs==nil||len(value.Outputs)>128{return invalid()}
 if _,err:=expectedRevision(value.Revision);err!=nil{return invalid()}
 switch value.State{case "cancelling","cancelled","completed","failed":default:return invalid()}
 if value.ErrorCode!=nil&&(value.State!="failed"||len(*value.ErrorCode)<1||len(*value.ErrorCode)>80){return invalid()}
 seen:=make(map[uuid.UUID]bool)
 for _,ref:=range value.Outputs{if ref.ArtifactID==uuid.Nil||seen[ref.ArtifactID]{return invalid()};if _,err:=domain.ParseDigest(ref.SHA256);err!=nil{return invalid()};seen[ref.ArtifactID]=true}
 return nil
}
