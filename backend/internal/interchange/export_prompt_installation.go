package interchange

import (
 "context"
 "encoding/json"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

type ExportPromptMaskPolicyFactory interface {
 BuildExportPromptMaskPolicy(context.Context,CreateIntent,FrozenExportDocuments,ExportDocumentCatalog,ImportDocumentObjects)(ExportSeriesMaskPolicyAuthority,error)
}
// Expected and Report are selected by deployment operators, never the archive.
// The request's immutable snapshot supplies the complete source binding set.
type SQLExportPromptMaskPolicyInstallation struct {
 Expected v2qualification.Expected
 Report v2input.Ref
}
func(s SQLExportPromptMaskPolicyInstallation)BuildExportPromptMaskPolicy(ctx context.Context,intent CreateIntent,frozen FrozenExportDocuments,catalog ExportDocumentCatalog,objects ImportDocumentObjects)(ExportSeriesMaskPolicyAuthority,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 scoped,ok:=catalog.(SQLExportDocumentCatalog);if !ok||scoped.DB==nil||objects==nil{return nil,v2input.ErrInvalid}
 actual,err:=FreezeCreateIntent(scoped.Tenant,scoped.Who,scoped.Request,scoped.Key);if err!=nil{return nil,err}
 if actual.RequestSHA!=intent.RequestSHA||actual.Identity!=intent.Identity{return nil,v2input.ErrInvalid}
 checked,_,err:=DecodeFrozenExportDocuments(ctx,intent,frozen.Body());if err!=nil{return nil,err}
 if string(scoped.Frozen.Body())!=string(checked.Body()){return nil,v2input.ErrInvalid}
 var payload exportFreezePayload;if err:=json.Unmarshal(checked.Body(),&payload);err!=nil{return nil,err}
 var snapshot v2input.SessionInputSnapshot;if err:=decodeImportDocument([]byte(payload.SnapshotJCS),&snapshot);err!=nil{return nil,err}
 bindings:=make([]v2input.SessionVersionBinding,0,len(snapshot.Items))
 for _,item:=range snapshot.Items{bindings=append(bindings,item.Binding)}
 if err:=validateExportMaskAuthorityInstallation(ctx,scoped.DB);err!=nil{return nil,err}
 return NewExportPromptMaskPolicy(scoped,objects,bindings,s.Report,s.Expected)
}
var _ ExportPromptMaskPolicyFactory=SQLExportPromptMaskPolicyInstallation{}
