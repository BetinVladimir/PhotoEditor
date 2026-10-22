package interchange

import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// The operation qualifier must attest original producer/protection provenance,
// and again the prepared target closure. An automatic policy is never a review.
type ImportConservativeParamsCodec struct {
 Qualifier ImportLayerOperationQualifier
 Masks ImportDependencyCodec
 Fallback ImportDependencyCodec
}

type importConservativeParams struct {
 Policy string `json:"policy"`
 Image v2input.Ref `json:"image"`
 Proposal *v2input.Ref `json:"proposal"`
 ProposalReport *v2input.Ref `json:"proposal_report,omitempty"`
 Resolved v2input.Ref `json:"resolved"`
 Protected []json.RawMessage `json:"protected,omitempty"`
}

func isImportConservativeParams(kind string,raw []byte)bool{
 if kind!="Params"{return false}
 var fields map[string]json.RawMessage
 if json.Unmarshal(raw,&fields)!=nil{return false}
 var policy string
 _ = json.Unmarshal(fields["policy"],&policy)
 return fields["policy"]!=nil&&(policy=="conservative_auto_v1"||fields["image"]!=nil&&fields["resolved"]!=nil&&(fields["proposal"]!=nil||fields["proposal_report"]!=nil||fields["protected"]!=nil))
}

func decodeImportConservativeParams(raw []byte)(importConservativeParams,string,error){
 var p importConservativeParams
 if len(raw)<2||len(raw)>1<<20{return p,"",v2input.ErrInvalid}
 normalized,err:=canonical.Bytes(raw)
 if err!=nil||!bytes.Equal(normalized,raw){return p,"",v2input.ErrInvalid}
 operation,err:=exportParamsOperationShape(raw)
 if err!=nil||(operation!="defects.auto_repair"&&operation!="sensor.dust_auto_repair"){return p,"",v2input.ErrInvalid}
 if decodeImportDocument(raw,&p)!=nil||p.Policy!="conservative_auto_v1"||!validImportRef(p.Image)||!validImportRef(p.Resolved){return p,"",v2input.ErrInvalid}
 if p.Proposal!=nil&&!validImportRef(*p.Proposal){return p,"",v2input.ErrInvalid}
 if operation=="defects.auto_repair"{
  if p.Proposal==nil||p.ProposalReport==nil||!validImportRef(*p.ProposalReport){return p,"",v2input.ErrInvalid}
 }else{
  if len(p.Protected)<1||len(p.Protected)>64{return p,"",v2input.ErrInvalid}
  for _,rawMask:=range p.Protected{mask,err:=review.ParseMaskRef(rawMask);if err!=nil||!mask.Protected{return p,"",v2input.ErrInvalid}}
 }
 return p,operation,nil
}

func(c ImportConservativeParamsCodec)RemapImportDocument(ctx context.Context,kind string,raw []byte,old,target v2input.SessionVersionBinding,resolve func(v2input.Ref,string)(v2input.Ref,error))([]byte,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if !isImportConservativeParams(kind,raw){if c.Fallback!=nil{return c.Fallback.RemapImportDocument(ctx,kind,raw,old,target,resolve)};return nil,&ImportRecipeUnsupported{Reason:"params_contract_not_installed"}}
 p,operation,err:=decodeImportConservativeParams(raw);if err!=nil{return nil,err}
 if c.Qualifier==nil||resolve==nil||p.Image!=old.Source||!validImportRef(target.Source)||old.AssetVersionID==uuid.Nil||target.AssetVersionID==uuid.Nil||old.AssetVersionID==target.AssetVersionID{return nil,v2input.ErrInvalid}
 if old.CanvasHash!=target.CanvasHash{return nil,&ImportRecipeUnsupported{Reason:"params_canvas_remap_required"}}
 // No archived claim can authorize its own protection or source image.
 if err:=c.Qualifier.QualifyImportedLayerOperation(ctx,operation,"1",append([]byte(nil),raw...),old);err!=nil{return nil,err}
 var fields map[string]json.RawMessage
 if json.Unmarshal(raw,&fields)!=nil{return nil,v2input.ErrInvalid}
 remap:=func(name,contract string)error{
  var original v2input.Ref
  if json.Unmarshal(fields[name],&original)!=nil||!validImportRef(original){return v2input.ErrInvalid}
  mapped,err:=resolve(original,contract);if err!=nil{return err}
  if !validImportRef(mapped)||mapped.ArtifactID==original.ArtifactID{return v2input.ErrInvalid}
  fields[name],err=canonical.Marshal(mapped);return err
 }
 if p.Proposal!=nil{contract:="DustCandidates";if operation=="defects.auto_repair"{contract="DefectCandidates"};if err:=remap("proposal",contract);err!=nil{return nil,err}}
 if operation=="defects.auto_repair"{if err:=remap("proposal_report","DefectProposeReport");err!=nil{return nil,err}}
 if err:=remap("resolved","ResolvedStyle");err!=nil{return nil,err}
 if operation=="sensor.dust_auto_repair"{
  if c.Masks==nil{return nil,&ImportRecipeUnsupported{Reason:"automatic_protection_authority_not_installed"}}
  masks:=make([]json.RawMessage,len(p.Protected))
  for i,mask:=range p.Protected{mapped,err:=c.Masks.RemapImportDocument(ctx,"MaskRef",mask,old,target,resolve);if err!=nil{return nil,err};masks[i]=mapped}
  fields["protected"],err=canonical.Marshal(masks);if err!=nil{return nil,err}
 }
 fields["image"],err=canonical.Marshal(target.Source);if err!=nil{return nil,err}
 prepared,err:=canonical.Marshal(fields);if err!=nil{return nil,err}
 if err:=c.QualifyRemappedImportDocument(ctx,kind,prepared,target);err!=nil{return nil,err}
 return prepared,ctx.Err()
}

func(c ImportConservativeParamsCodec)QualifyRemappedImportDocument(ctx context.Context,kind string,raw []byte,target v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 if !isImportConservativeParams(kind,raw){if c.Fallback!=nil{return c.Fallback.QualifyRemappedImportDocument(ctx,kind,raw,target)};return &ImportRecipeUnsupported{Reason:"params_contract_not_installed"}}
 p,operation,err:=decodeImportConservativeParams(raw);if err!=nil{return err}
 if c.Qualifier==nil||p.Image!=target.Source||target.AssetVersionID==uuid.Nil||!validImportRef(target.Source){return v2input.ErrInvalid}
 if operation=="sensor.dust_auto_repair"{
  if c.Masks==nil{return &ImportRecipeUnsupported{Reason:"automatic_protection_authority_not_installed"}}
  for _,mask:=range p.Protected{if err:=c.Masks.QualifyRemappedImportDocument(ctx,"MaskRef",mask,target);err!=nil{return err}}
 }
 if err:=c.Qualifier.QualifyImportedLayerOperation(ctx,operation,"1",append([]byte(nil),raw...),target);err!=nil{return err}
 return ctx.Err()
}
func(c ImportConservativeParamsCodec)BindImportDocumentSource(source v2input.Ref)ImportDependencyCodec{
 if binder,ok:=c.Fallback.(ImportDocumentSourceBinder);ok{c.Fallback=binder.BindImportDocumentSource(source)}
 return c
}
