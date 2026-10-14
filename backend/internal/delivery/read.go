package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
 "strconv"
 "time"
	"unicode/utf8"
 "photoeditor/backend/internal/v2input"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/pagination"
	"photoeditor/backend/internal/tenant"
)

type Service struct{ DB *pgxpool.Pool; Cursors pagination.Codec }

// ReadPackage belongs in a tenant-authenticated API transaction. SQL repeats
// current session/tenant/project authorization and returns no storage locations.
func ReadPackage(ctx context.Context, tx ProofTransaction, who auth.Identity, tid, id uuid.UUID) (json.RawMessage, error) {
	if tx == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tid == uuid.Nil || id == uuid.Nil {
		return nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT photo.get_v30_delivery($1,$2,$3,$4)", tid, who.UserID, who.SessionID, id).Scan(&raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, domain.NewError("NOT_FOUND", 404)
	}
	if !ValidPackageResource(raw,tid,id) {
		return nil, domain.NewError("DELIVERY_DOCUMENT_INVALID", 500)
	}
	if err:=ctx.Err();err!=nil{return nil,err}
 return append(json.RawMessage(nil), raw...), nil
}

func (s Service) Get(ctx context.Context, who auth.Identity, tid, id uuid.UUID) (json.RawMessage, error) {
	if s.DB == nil {
		return nil, domain.NewError("DELIVERY_UNAVAILABLE", 503)
	}
	var result json.RawMessage
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, _ domain.Role) error {
		var err error
		result, err = ReadPackage(ctx, tx, who, tid, id)
		return err
	})
	return result, err
}

// Validate public identity without treating metadata as physical readiness proof.
func closedJSONObject(raw []byte)(map[string]json.RawMessage,bool){
 decoder:=json.NewDecoder(bytes.NewReader(raw))
 token,err:=decoder.Token()
 if err!=nil||token!=json.Delim('{'){return nil,false}
 fields:=make(map[string]json.RawMessage)
 for decoder.More(){
  keyToken,err:=decoder.Token()
  if err!=nil{return nil,false}
  key,ok:=keyToken.(string)
  if !ok{return nil,false}
  if _,exists:=fields[key];exists{return nil,false}
  var value json.RawMessage
  if decoder.Decode(&value)!=nil{return nil,false}
  fields[key]=value
 }
 token,err=decoder.Token()
 if err!=nil||token!=json.Delim('}'){return nil,false}
 _,err=decoder.Token()
 return fields,err==io.EOF
}

func ValidPackageResource(raw []byte,tid,id uuid.UUID)bool{
 if len(raw)<2||len(raw)>1<<20{return false}
 root,ok:=closedJSONObject(raw)
 if !ok||len(root)!=4{return false}
 for _,key:=range []string{"id","revision","document","data"}{if _,ok:=root[key];!ok{return false}}
 dataFields,ok:=closedJSONObject(root["data"])
 if !ok{return false}
 allowed:=map[string]bool{"resource_type":true,"id":true,"tenant_id":true,"project_id":true,"created_at":true,"revision":true,"updated_at":true,"workflow_id":true,"snapshot_id":true,"name":true,"policy_artifact_id":true,"manifest_artifact_id":true,"published_at":true,"state":true}
 if len(dataFields)!=len(allowed){return false}
 for field:=range dataFields{if !allowed[field]{return false}}
 var resource struct{
  ID uuid.UUID `json:"id"`
  Revision string `json:"revision"`
  Document *v2input.Ref `json:"document"`
  Data struct{
   ID uuid.UUID `json:"id"`
   Tenant uuid.UUID `json:"tenant_id"`
   Project uuid.UUID `json:"project_id"`
   Created time.Time `json:"created_at"`
   Revision string `json:"revision"`
   Updated time.Time `json:"updated_at"`
   Workflow uuid.UUID `json:"workflow_id"`
   Snapshot uuid.UUID `json:"snapshot_id"`
   Name string `json:"name"`
   Policy uuid.UUID `json:"policy_artifact_id"`
   Type string `json:"resource_type"`
   Manifest uuid.UUID `json:"manifest_artifact_id"`
   State string `json:"state"`
   Published *time.Time `json:"published_at"`
  } `json:"data"`
 }
 if json.Unmarshal(raw,&resource)!=nil{return false}
 revision,err:=strconv.ParseInt(resource.Revision,10,64)
 if err!=nil||revision<1||revision>9007199254740991||strconv.FormatInt(revision,10)!=resource.Revision||resource.ID!=id||resource.Data.ID!=id||resource.Data.Tenant!=tid||resource.Data.Revision!=resource.Revision||resource.Data.Type!="delivery_packages"{return false}
 if resource.Data.Project==uuid.Nil||resource.Data.Workflow==uuid.Nil||resource.Data.Snapshot==uuid.Nil||resource.Data.Policy==uuid.Nil||resource.Data.Created.IsZero()||resource.Data.Updated.IsZero()||resource.Data.Updated.Before(resource.Data.Created)||!utf8.ValidString(resource.Data.Name)||utf8.RuneCountInString(resource.Data.Name)<1||utf8.RuneCountInString(resource.Data.Name)>200{return false}
 switch resource.Data.State{case "building","review","ready","failed","cancelled":default:return false}
 if (resource.Data.State=="review"||resource.Data.State=="ready")&&resource.Data.Manifest==uuid.Nil{return false}
 if resource.Data.Published!=nil&&(resource.Data.Published.IsZero()||resource.Data.State!="ready"||resource.Data.Manifest==uuid.Nil){return false}
 if resource.Document!=nil{
  fields,ok:=closedJSONObject(root["document"])
  if !ok||len(fields)!=2||resource.Document.ArtifactID==uuid.Nil||resource.Document.ArtifactID!=resource.Data.Manifest||!shaPattern.MatchString(resource.Document.SHA256){return false}
 }
 return true
}
