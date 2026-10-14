package interchange

import (
 "bytes"
 "encoding/json"
 "io"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/v2input"
)

type CreateRequest struct {
 ProjectID uuid.UUID `json:"project_id"`
 Direction string `json:"direction"`
 Format string `json:"format"`
 Bundle *v2input.Ref `json:"bundle,omitempty"`
 SnapshotID *uuid.UUID `json:"snapshot_id,omitempty"`
}

// Decode tokens explicitly: duplicate keys cannot silently select another input.
func (r *CreateRequest) UnmarshalJSON(raw []byte)error{
 if len(raw)>16384{return domain.NewError("VALIDATION_FAILED",422)}
 decoder:=json.NewDecoder(bytes.NewReader(raw))
 start,err:=decoder.Token();if err!=nil||start!=json.Delim('{'){return domain.NewError("VALIDATION_FAILED",422)}
 fields:=map[string]json.RawMessage{}
 for decoder.More(){
  token,err:=decoder.Token();if err!=nil{return err};key,ok:=token.(string);if !ok{return domain.NewError("VALIDATION_FAILED",422)}
  if _,exists:=fields[key];exists{return domain.NewError("VALIDATION_FAILED",422)}
  var value json.RawMessage;if err:=decoder.Decode(&value);err!=nil{return err}
  if bytes.Equal(bytes.TrimSpace(value),[]byte("null")){return domain.NewError("VALIDATION_FAILED",422)}
  fields[key]=value
 }
 if _,err:=decoder.Token();err!=nil{return err};if decoder.Decode(new(any))!=io.EOF{return domain.NewError("VALIDATION_FAILED",422)}
 var decoded struct {ProjectID uuid.UUID `json:"project_id"`;Direction string `json:"direction"`;Format string `json:"format"`;Bundle json.RawMessage `json:"bundle"`;SnapshotID *uuid.UUID `json:"snapshot_id"`}
 if err:=json.Unmarshal(raw,&decoded);err!=nil{return err}
 if len(fields)!=4{return domain.NewError("VALIDATION_FAILED",422)}
 for _,key:=range []string{"project_id","direction","format"}{if _,ok:=fields[key];!ok{return domain.NewError("VALIDATION_FAILED",422)}}
 result:=CreateRequest{ProjectID:decoded.ProjectID,Direction:decoded.Direction,Format:decoded.Format,SnapshotID:decoded.SnapshotID}
 if decoded.Direction=="import" {
  if _,ok:=fields["bundle"];!ok{return domain.NewError("VALIDATION_FAILED",422)}
  var ref map[string]json.RawMessage
  // Canonical parsing rejects duplicate nested reference keys as well.
  normalized,err:=canonical.Bytes(decoded.Bundle);if err!=nil{return err}
  if err:=json.Unmarshal(normalized,&ref);err!=nil{return err}
  if len(ref)!=2||ref["artifact_id"]==nil||ref["sha256"]==nil{return domain.NewError("VALIDATION_FAILED",422)}
  var bundle v2input.Ref;if err:=json.Unmarshal(normalized,&bundle);err!=nil{return err};result.Bundle=&bundle
 } else if decoded.Direction=="export" {
  if _,ok:=fields["snapshot_id"];!ok{return domain.NewError("VALIDATION_FAILED",422)}
 } else{return domain.NewError("VALIDATION_FAILED",422)}
 if err:=result.validate();err!=nil{return err};*r=result;return nil
}
func(r CreateRequest)validate()error{
 invalid:=func()error{return domain.NewError("VALIDATION_FAILED",422)}
 if r.ProjectID==uuid.Nil||(r.Format!="photoeditor_bundle"&&r.Format!="xmp"&&r.Format!="captureone_sidecar"){return invalid()}
 switch r.Direction {
 case "import":
  if r.Bundle==nil||r.SnapshotID!=nil||r.Bundle.ArtifactID==uuid.Nil{return invalid()}
  if _,err:=domain.ParseDigest(r.Bundle.SHA256);err!=nil{return invalid()}
 case "export":
  if r.Bundle!=nil||r.SnapshotID==nil||*r.SnapshotID==uuid.Nil{return invalid()}
 default:return invalid()
 }
 return nil
}

type CreateIdentity struct {Run,Workflow,Task,Snapshot,Params uuid.UUID}
type CreateIntent struct {Request CreateRequest;Identity CreateIdentity;RequestSHA string;RequestJCS []byte}

// Deterministic identity depends on tenant/actor/key, while the ledger hash
// distinguishes conflicting request bodies. Session rotation keeps the identity.
func FreezeCreateIntent(tenant uuid.UUID,who auth.Identity,request CreateRequest,key string)(CreateIntent,error){
 if tenant==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||!idempotency.ValidKey(key){return CreateIntent{},domain.NewError("VALIDATION_FAILED",422)}
 if err:=request.validate();err!=nil{return CreateIntent{},err}
 if request.Bundle!=nil{owned:=*request.Bundle;request.Bundle=&owned}
 if request.SnapshotID!=nil{owned:=*request.SnapshotID;request.SnapshotID=&owned}
 raw,err:=canonical.Marshal(request);if err!=nil{return CreateIntent{},err}
 hash,err:=canonical.Hash(request);if err!=nil{return CreateIntent{},err}
 run:=uuid.NewSHA1(tenant,[]byte("interchange:create:"+who.UserID.String()+":"+key))
 workflow:=uuid.NewSHA1(run,[]byte("workflow"))
 operation:="v2.interchange.parse:0";if request.Direction=="export"{operation="v2.interchange.export:0"}
 task:=uuid.NewSHA1(workflow,[]byte(operation))
 snapshot:=uuid.NewSHA1(workflow,[]byte("interchange:artifact-snapshot:1"));if request.Direction=="export"{snapshot=*request.SnapshotID}
 return CreateIntent{Request:request,Identity:CreateIdentity{run,workflow,task,snapshot,uuid.NewSHA1(task,[]byte("interchange:params:1"))},RequestSHA:hash,RequestJCS:raw},nil
}
