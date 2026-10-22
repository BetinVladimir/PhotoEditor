package v2draft

import (
 "context"
 "encoding/json"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/editor"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/tenant"
)

type EditorRecipeBridgeRequest struct {
 Revision string `json:"revision"`
 Params json.RawMessage `json:"params"`
 DocumentID uuid.UUID `json:"document_id"`
}
func checkEditorBridge(ctx context.Context,tx pgx.Tx,who auth.Identity,tenant,version uuid.UUID,bridge *EditorRecipeBridgeRequest) error {
 revision,err:=replaceRevision(bridge.Revision);if err!=nil||revision<1{return domain.NewError("VALIDATION_FAILED",422)}
 var actual int64;var raw json.RawMessage
 err=tx.QueryRow(ctx,`SELECT revision,params FROM photo.editor_drafts WHERE tenant_id=$1 AND asset_version_id=$2 AND user_id=$3 FOR SHARE`,tenant,version,who.UserID).Scan(&actual,&raw)
 if errors.Is(err,pgx.ErrNoRows){return domain.NewError("REVISION_CONFLICT",409)};if err!=nil{return err}
 expected,e:=canonical.Bytes(bridge.Params);if e!=nil{return domain.NewError("VALIDATION_FAILED",422)}
 persisted,e:=canonical.Bytes(raw);if e!=nil{return e}
 if actual!=revision||string(expected)!=string(persisted){return domain.NewError("REVISION_CONFLICT",409)}
 return nil
}
func compileEditorBridgeOverride(params json.RawMessage,source string,revision int64)([]byte,error){
 var envelope struct{Schema int `json:"schema_version"`;Modules json.RawMessage `json:"modules"`};var fields map[string]json.RawMessage
 if json.Unmarshal(params,&fields)!=nil||len(fields)!=2||json.Unmarshal(params,&envelope)!=nil||envelope.Schema!=1||len(envelope.Modules)==0{return nil,domain.NewError("VALIDATION_FAILED",422)}
 body,err:=canonical.Marshal(map[string]any{"schema_version":1,"input_sha256":source,"revision":revision,"modules":envelope.Modules})
 if err!=nil{return nil,err};if err=editor.ValidateOverride(body,source);err!=nil{return nil,err};return body,nil
}
func replaceEditorBridgeRef(recipe json.RawMessage,previous *v2input.Ref,next v2input.Ref)(json.RawMessage,error){
 var fields map[string]json.RawMessage;var refs []v2input.Ref
 if json.Unmarshal(recipe,&fields)!=nil||json.Unmarshal(fields["overrides"],&refs)!=nil||len(refs)>128{return nil,domain.NewError("VALIDATION_FAILED",422)}
 result:=make([]v2input.Ref,0,len(refs)+1);found:=previous==nil
 for _,ref:=range refs{if previous!=nil&&ref==*previous{if found{return nil,domain.NewError("VALIDATION_FAILED",422)};found=true;continue};result=append(result,ref)}
 if !found||len(result)>=128{return nil,domain.NewError("REVISION_CONFLICT",409)}
 result=append(result,next);fields["overrides"],_=json.Marshal(result);return canonical.Marshal(fields)
}

func(s Service) prepareEditorRecipeBridge(ctx context.Context,who auth.Identity,tenantID,asset uuid.UUID,key string,expected int64,req ReplaceRequest,binding v2input.SessionVersionBinding,source string)(json.RawMessage,*v2input.Ref,error){
 bridge:=req.EditorBridge
 if bridge==nil{return req.Recipe,nil,nil}
 if expected<1||bridge.DocumentID==uuid.Nil{return nil,nil,domain.NewError("VALIDATION_FAILED",422)}
 var previous *v2input.Ref
 err:=tenant.WithTenant(ctx,s.DB,who,tenantID,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  if err:=checkEditorBridge(ctx,tx,who,tenantID,binding.AssetVersionID,bridge);err!=nil{return err}
  var current uuid.UUID;var rev int64
  if err:=tx.QueryRow(ctx,`SELECT current_document_id,revision FROM photo.v2_document_streams WHERE tenant_id=$1 AND asset_version_id=$2 AND stream_type='draft'`,tenantID,binding.AssetVersionID).Scan(&current,&rev);err!=nil{return err}
  if current!=bridge.DocumentID||rev!=expected{return domain.NewError("REVISION_CONFLICT",409)}
  var ref v2input.Ref
  err:=tx.QueryRow(ctx,`SELECT b.override_artifact_id,b.override_sha FROM photo.editor_recipe_bridges b JOIN photo.v2_documents d ON d.tenant_id=b.tenant_id AND d.id=b.document_id JOIN photo.v2_documents current ON current.tenant_id=d.tenant_id AND current.id=$3 AND current.stream_id=d.stream_id WHERE b.tenant_id=$1 AND b.asset_version_id=$2 AND d.version_no<=current.version_no ORDER BY d.version_no DESC LIMIT 1`,tenantID,binding.AssetVersionID,current).Scan(&ref.ArtifactID,&ref.SHA256)
  if errors.Is(err,pgx.ErrNoRows){return nil};if err!=nil{return err}
  var recipe struct{Overrides []v2input.Ref `json:"overrides"`};if json.Unmarshal(req.Recipe,&recipe)!=nil{return domain.NewError("VALIDATION_FAILED",422)}
  for _,item:=range recipe.Overrides{if item==ref{previous=&ref;break}}
  return nil
 });if err!=nil{return nil,nil,err}
 revision,_:=replaceRevision(bridge.Revision);body,err:=compileEditorBridgeOverride(bridge.Params,source,revision);if err!=nil{return nil,nil,err}
 ref,err:=(Publisher{DB:s.DB,Objects:s.Objects,Bucket:s.Bucket}).Publish(ctx,who,tenantID,binding.AssetVersionID,stableDraftID(tenantID,who.UserID,asset,key,"editor-override"),body);if err!=nil{return nil,nil,err}
 recipe,err:=replaceEditorBridgeRef(req.Recipe,previous,ref);return recipe,&ref,err
}
