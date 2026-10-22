package interchange
import (
 "context"
 "encoding/json"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
type ExportReportMaskAuthority interface{QualifyExportReportMask(context.Context,v2input.Entry,[]byte,v2input.Ref)error}
func exportReportInlineMasks(entry v2input.Entry,raw []byte)([]json.RawMessage,error){
 if entry.Contract!="DefectProposeReport"||!validImportRef(entry.Ref)||rawSHA(raw)!=entry.Ref.SHA256{return nil,v2input.ErrInvalid}
 // The closed report schema, including every optional mask, is validated by
 // the shared extraction before returning owned bytes.
 masks,err:=inlinePreparedMasks(raw);if err!=nil{return nil,err}
 result:=make([]json.RawMessage,len(masks));for i,mask:=range masks{result[i]=append(json.RawMessage(nil),mask...)}
 if len(result)==0{return nil,v2input.ErrInvalid};return result,nil
}
func(s SQLExportDocumentCatalog)QualifyExportReportMask(ctx context.Context,parent v2input.Entry,raw []byte,mask v2input.Ref)error{return qualifyExportReportMask(ctx,s,s,parent,raw,mask)}
func(s SQLExportResultInputCatalog)QualifyExportReportMask(ctx context.Context,parent v2input.Entry,raw []byte,mask v2input.Ref)error{return qualifyExportReportMask(ctx,s,s,parent,raw,mask)}
func qualifyExportReportMask(ctx context.Context,catalog ExportDocumentCatalog,authority ExportDefectPNGOccurrenceAuthority,parent v2input.Entry,raw []byte,mask v2input.Ref)error{
 if err:=ctx.Err();err!=nil{return err}
 if catalog==nil||authority==nil||!validImportRef(mask){return v2input.ErrInvalid}
 masks,err:=exportReportInlineMasks(parent,raw);if err!=nil{return err}
 found:=false
 for _,body:=range masks{parsed,err:=review.ParseMaskRef(body);if err!=nil{return err};if parsed.Artifact.ID==mask.ArtifactID{if parsed.Artifact.SHA256!=mask.SHA256{return v2input.ErrInvalid};found=true}}
 if !found{return v2input.ErrInvalid}
 var header struct{PNG v2input.Ref `json:"label_png"`};if json.Unmarshal(raw,&header)!=nil||!validImportRef(header.PNG){return v2input.ErrInvalid}
 leaf:=v2input.Entry{Ref:mask,Contract:"MaskPixels"}
 before,err:=catalog.LookupExportDocument(ctx,leaf);if err!=nil{return err}
 if err:=authority.QualifyExportDefectPNG(ctx,parent,append([]byte(nil),raw...),v2input.Entry{Ref:header.PNG,Contract:"DefectLabelPNG"});err!=nil{return err}
 after,err:=catalog.LookupExportDocument(ctx,leaf);if err!=nil{return err};if before!=after{return v2input.ErrInvalid}
 return ctx.Err()
}
