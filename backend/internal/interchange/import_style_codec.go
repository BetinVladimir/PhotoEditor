package interchange

import (
 "bytes"
 "context"
 "encoding/json"
 "strings"
 "unicode/utf8"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

// The qualifier validates the full StyleV2 schema and installed color/raw/model
// dependencies, including nested assets. DecodeStyleBaseline alone is not this
// authority: it only reads six scalar coordinates used by the recipe compiler.
type ImportStyleQualifier interface{
 ValidateArchivedImportStyle(context.Context,[]byte)error
 QualifyImportedStyle(context.Context,[]byte,v2input.SessionVersionBinding)error
}
// An asset resolver imports a portable background and returns its new portable
// identifier. Its implementation must publish/qualify exact image dependencies.
type ImportStyleBackgroundRemapper interface{RemapImportedStyleBackground(context.Context,v2input.Ref,string,v2input.SessionVersionBinding)(string,error)}
type ImportStyleCodec struct{Source v2input.Ref;Qualifier ImportStyleQualifier;Backgrounds ImportStyleBackgroundRemapper;Fallback ImportDependencyCodec}
// BindImportDocumentSource returns invocation-local state; shared codecs remain immutable.
func(c ImportStyleCodec)BindImportDocumentSource(source v2input.Ref)ImportDependencyCodec{
 c.Source=source
 if binder,ok:=c.Fallback.(ImportDocumentSourceBinder);ok{c.Fallback=binder.BindImportDocumentSource(source)}
 return c
}
func(c ImportStyleCodec)RemapImportDocument(ctx context.Context,contract string,raw []byte,old,target v2input.SessionVersionBinding,resolve func(v2input.Ref,string)(v2input.Ref,error))([]byte,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if contract!="StyleV2"{if c.Fallback!=nil{return c.Fallback.RemapImportDocument(ctx,contract,raw,old,target,resolve)};return nil,&ImportRecipeUnsupported{Reason:"dependency_contract_not_installed"}}
 if c.Qualifier==nil||target.AssetVersionID==uuid.Nil{return nil,v2input.ErrInvalid}
 normalized,err:=canonical.Bytes(raw);if err!=nil||!bytes.Equal(normalized,raw)||len(raw)>8<<20{return nil,v2input.ErrInvalid}
 if _,err:=v2draft.DecodeStyleBaseline(raw);err!=nil{return nil,err}
 // Reject unavailable operators/models before any optional asset publication.
 if err:=c.Qualifier.ValidateArchivedImportStyle(ctx,append([]byte(nil),raw...));err!=nil{return nil,err}
 var fields map[string]json.RawMessage;if err:=json.Unmarshal(raw,&fields);err!=nil{return nil,err}
 var oldID string;if err:=json.Unmarshal(fields["style_id"],&oldID);err!=nil||len(oldID)<1||len(oldID)>120{return nil,v2input.ErrInvalid}
 id:=uuid.NewSHA1(target.AssetVersionID,[]byte("interchange:style:"+rawSHA(raw)))
 fields["style_id"],err=canonical.Marshal(id.String());if err!=nil{return nil,err}
 var extensions map[string]json.RawMessage;if err:=json.Unmarshal(fields["extensions"],&extensions);err!=nil{return nil,err};if extensions==nil{return nil,v2input.ErrInvalid}
 if background,present:=extensions["background"];present&&string(background)!="null"{
  var value map[string]json.RawMessage;if err:=json.Unmarshal(background,&value);err!=nil{return nil,err}
  var mode string;if err:=json.Unmarshal(value["mode"],&mode);err!=nil{return nil,err}
  if mode=="asset"{
   if c.Backgrounds==nil{return nil,&ImportRecipeUnsupported{Reason:"background_asset_remapper_not_installed"}}
   var portable string;if err:=json.Unmarshal(value["asset_id"],&portable);err!=nil||portable==""{return nil,v2input.ErrInvalid}
   if !validImportRef(c.Source)||c.Source.SHA256!=rawSHA(raw){return nil,v2input.ErrInvalid}
   mapped,err:=c.Backgrounds.RemapImportedStyleBackground(ctx,c.Source,portable,target);if err!=nil{return nil,err}
   if mapped==""||mapped==portable||len(mapped)>120||!utf8.ValidString(mapped)||strings.TrimSpace(mapped)!=mapped{return nil,v2input.ErrInvalid}
   value["asset_id"],err=canonical.Marshal(mapped);if err!=nil{return nil,err}
   extensions["background"],err=canonical.Marshal(value);if err!=nil{return nil,err}
   fields["extensions"],err=canonical.Marshal(extensions);if err!=nil{return nil,err}
  }
 }
 // Imported measurement claims are historical metadata, not evidence that the
 // new installation/source has passed local validation. No processing field
 // is changed by resetting these administrative claims.
 var provenance map[string]json.RawMessage;if err:=json.Unmarshal(fields["provenance"],&provenance);err!=nil{return nil,err};if provenance==nil{return nil,v2input.ErrInvalid}
 for _,key:=range []string{"pair_count","session_count"}{provenance[key]=json.RawMessage(`0`)}
 for _,key:=range []string{"manifest_sha256","runtime_lock_id"}{provenance[key]=json.RawMessage(`null`)}
 fields["provenance"],err=canonical.Marshal(provenance);if err!=nil{return nil,err}
 var quality map[string]json.RawMessage;if err:=json.Unmarshal(fields["quality"],&quality);err!=nil{return nil,err};if quality==nil{return nil,v2input.ErrInvalid}
 quality["status"]=json.RawMessage(`"needs_review"`);quality["transfer_confidence"]=json.RawMessage(`"unvalidated"`)
 for _,key:=range []string{"reconstruction","validation","beauty_validation","geometry_validation"}{if _,present:=quality[key];present{quality[key]=json.RawMessage(`null`)}}
 var warnings []string;if err:=json.Unmarshal(quality["warnings"],&warnings);err!=nil{return nil,err}
 const warning="imported_style_requires_validation";found:=false;for _,item:=range warnings{if item==warning{found=true}}
 if !found{warnings=append(warnings,warning)}
 quality["warnings"],err=canonical.Marshal(warnings);if err!=nil{return nil,err}
 fields["quality"],err=canonical.Marshal(quality);if err!=nil{return nil,err}
 prepared,err:=canonical.Marshal(fields);if err!=nil{return nil,err}
 if err:=c.QualifyRemappedImportDocument(ctx,contract,prepared,target);err!=nil{return nil,err}
 return prepared,ctx.Err()
}
func(c ImportStyleCodec)QualifyRemappedImportDocument(ctx context.Context,contract string,raw []byte,target v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 if contract!="StyleV2"{if c.Fallback!=nil{return c.Fallback.QualifyRemappedImportDocument(ctx,contract,raw,target)};return &ImportRecipeUnsupported{Reason:"dependency_contract_not_installed"}}
 if c.Qualifier==nil{return v2input.ErrInvalid}
 if _,err:=v2draft.DecodeStyleBaseline(raw);err!=nil{return err}
 if err:=c.Qualifier.QualifyImportedStyle(ctx,append([]byte(nil),raw...),target);err!=nil{return err}
 return ctx.Err()
}
