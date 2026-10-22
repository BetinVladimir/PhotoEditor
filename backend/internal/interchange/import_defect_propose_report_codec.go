package interchange

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

// Authority proves the original report/proposal/protection producer together,
// and physical PNG/label-map equality, landmark ancestry and protection policy
// in the prepared closure. It must not infer these from report JSON alone.
type ImportDefectProposeReportQualifier interface{
 ValidateArchivedImportDefectProposeReport(context.Context,[]byte,v2input.SessionVersionBinding)error
 QualifyImportedDefectProposeReport(context.Context,[]byte,v2input.SessionVersionBinding)error
}
type ImportDefectProposeReportCodec struct{Qualifier ImportDefectProposeReportQualifier;Masks ImportDependencyCodec}

func validateImportDefectProposeReport(raw []byte,source v2input.Ref)error{
 if len(raw)<2||len(raw)>1<<20||!validImportRef(source){return v2input.ErrInvalid}
 var header struct{Params struct{Image v2input.Ref `json:"image"`} `json:"params"`}
 if json.Unmarshal(raw,&header)!=nil||header.Params.Image!=source{return v2input.ErrInvalid}
 if _,err:=v2draft.DocumentChildren(raw,"DefectProposeReport",v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)});err!=nil{return err}
 return nil
}
func(c ImportDefectProposeReportCodec)RemapImportDocument(ctx context.Context,kind string,raw []byte,old,target v2input.SessionVersionBinding,resolve func(v2input.Ref,string)(v2input.Ref,error))([]byte,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if kind!="DefectProposeReport"{return nil,&ImportRecipeUnsupported{Reason:"dependency_contract_not_installed"}}
 if c.Qualifier==nil||c.Masks==nil{return nil,&ImportRecipeUnsupported{Reason:"defect_propose_report_authority_not_installed"}}
 if resolve==nil||old.AssetVersionID==uuid.Nil||target.AssetVersionID==uuid.Nil||old.AssetVersionID==target.AssetVersionID||!validImportRef(target.Source){return nil,v2input.ErrInvalid}
 if old.CanvasHash!=target.CanvasHash{return nil,&ImportRecipeUnsupported{Reason:"defect_propose_report_canvas_remap_required"}}
 if err:=validateImportDefectProposeReport(raw,old.Source);err!=nil{return nil,err}
 if err:=c.Qualifier.ValidateArchivedImportDefectProposeReport(ctx,append([]byte(nil),raw...),old);err!=nil{return nil,err}
 var fields,params map[string]json.RawMessage
 if json.Unmarshal(raw,&fields)!=nil||json.Unmarshal(fields["params"],&params)!=nil{return nil,v2input.ErrInvalid}
 remapRef:=func(values map[string]json.RawMessage,key,contract string)error{
  var original v2input.Ref;if json.Unmarshal(values[key],&original)!=nil||!validImportRef(original){return v2input.ErrInvalid}
  mapped,err:=resolve(original,contract);if err!=nil{return err}
  if !validImportRef(mapped)||mapped.ArtifactID==original.ArtifactID{return v2input.ErrInvalid}
  values[key],err=canonical.Marshal(mapped);return err
 }
 remapMask:=func(values map[string]json.RawMessage,key string)error{
  rawMask:=values[key];if len(rawMask)==0||string(rawMask)=="null"{return nil}
  mapped,err:=c.Masks.RemapImportDocument(ctx,"MaskRef",rawMask,old,target,resolve);if err!=nil{return err};values[key]=mapped;return nil
 }
 remapObject:=func(values map[string]json.RawMessage)error{
  var original uuid.UUID;if json.Unmarshal(values["object_id"],&original)!=nil||original==uuid.Nil{return v2input.ErrInvalid}
  mapped:=uuid.NewSHA1(target.AssetVersionID,[]byte("interchange:mask-object:"+original.String()))
  encoded,err:=canonical.Marshal(mapped);if err!=nil{return err};values["object_id"]=encoded;return nil
 }
 if err:=remapRef(fields,"proposal","DefectCandidates");err!=nil{return nil,err}
 if err:=remapRef(fields,"label_png","DefectLabelPNG");err!=nil{return nil,err}
 if err:=remapMask(fields,"protection");err!=nil{return nil,err}
 if err:=remapMask(params,"permanent");err!=nil{return nil,err}
 var faces []map[string]json.RawMessage
 if json.Unmarshal(params["faces"],&faces)!=nil{return nil,v2input.ErrInvalid}
 for _,face:=range faces{
  if err:=remapRef(face,"landmarks","FaceLandmarks");err!=nil{return nil,err}
  if err:=remapObject(face);err!=nil{return nil,err}
  for _,key:=range []string{"manual","skin"}{if err:=remapMask(face,key);err!=nil{return nil,err}}
 }
 var err error
 params["faces"],err=canonical.Marshal(faces);if err!=nil{return nil,err}
 if rawMasks,exists:=params["protections"];exists{
  var masks []json.RawMessage;if json.Unmarshal(rawMasks,&masks)!=nil{return nil,v2input.ErrInvalid}
  for i,mask:=range masks{mapped,err:=c.Masks.RemapImportDocument(ctx,"MaskRef",mask,old,target,resolve);if err!=nil{return nil,err};masks[i]=mapped}
  params["protections"],err=canonical.Marshal(masks);if err!=nil{return nil,err}
 }
 params["image"],err=canonical.Marshal(target.Source);if err!=nil{return nil,err}
 fields["params"],err=canonical.Marshal(params);if err!=nil{return nil,err}
 var evidence []map[string]json.RawMessage
 if json.Unmarshal(fields["faces"],&evidence)!=nil{return nil,v2input.ErrInvalid}
 for _,face:=range evidence{if err:=remapObject(face);err!=nil{return nil,err}}
 fields["faces"],err=canonical.Marshal(evidence);if err!=nil{return nil,err}
 prepared,err:=canonical.Marshal(fields);if err!=nil{return nil,err}
 if err:=c.QualifyRemappedImportDocument(ctx,kind,prepared,target);err!=nil{return nil,err}
 return prepared,ctx.Err()
}
func(c ImportDefectProposeReportCodec)QualifyRemappedImportDocument(ctx context.Context,kind string,raw []byte,target v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 if kind!="DefectProposeReport"{return &ImportRecipeUnsupported{Reason:"dependency_contract_not_installed"}}
 if c.Qualifier==nil||c.Masks==nil{return &ImportRecipeUnsupported{Reason:"defect_propose_report_authority_not_installed"}}
 if target.AssetVersionID==uuid.Nil{return v2input.ErrInvalid}
 if err:=validateImportDefectProposeReport(raw,target.Source);err!=nil{return err}
 var document struct{
  Protection json.RawMessage `json:"protection"`
  Params struct{Permanent json.RawMessage `json:"permanent"`;Protections []json.RawMessage `json:"protections"`;Faces []struct{Skin json.RawMessage `json:"skin"`;Manual json.RawMessage `json:"manual"`} `json:"faces"`} `json:"params"`
 }
 if json.Unmarshal(raw,&document)!=nil{return v2input.ErrInvalid}
 masks:=[]json.RawMessage{document.Protection,document.Params.Permanent}
 masks=append(masks,document.Params.Protections...)
 for _,face:=range document.Params.Faces{masks=append(masks,face.Skin,face.Manual)}
 for _,mask:=range masks{if len(mask)!=0&&string(mask)!="null"{if err:=c.Masks.QualifyRemappedImportDocument(ctx,"MaskRef",mask,target);err!=nil{return err}}}
 if err:=c.Qualifier.QualifyImportedDefectProposeReport(ctx,append([]byte(nil),raw...),target);err!=nil{return err}
 return ctx.Err()
}
