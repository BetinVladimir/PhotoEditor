package interchange

import (
 "bytes"
 "context"
 "photoeditor/backend/internal/v2input"
)

// Content proof supplements canonical completed producer metadata. Original
// measure image/mask/source semantics remain an independent mandatory policy.
type NativeExportSeriesOriginQualifier struct{Reader ExportSeriesOriginReader;Metadata ExportSeriesAuthority;Inputs ExportSeriesStatInputs;native *NativeExportStyleValidator}
func NewNativeExportSeriesOriginQualifier(reader ExportSeriesOriginReader,metadata ExportSeriesAuthority,inputs ExportSeriesStatInputs,scratch *ExportArchiveScratch,probe ExportNativeProbe,models map[string]string,runtime string)(*NativeExportSeriesOriginQualifier,error){
 if reader.Source==nil||reader.Objects==nil||metadata==nil||inputs.Authority==nil||inputs.Images==nil||inputs.Measure==nil{return nil,v2input.ErrInvalid}
 native,err:=NewNativeExportStyleValidator(scratch,probe,models,runtime,exportCompositionStagingValidator{});if err!=nil{return nil,err};native.contract="SeriesOrigin"
 return &NativeExportSeriesOriginQualifier{Reader:reader,Metadata:metadata,Inputs:inputs,native:native},nil
}
func(q *NativeExportSeriesOriginQualifier)QualifyExportSeries(ctx context.Context,entry v2input.Entry,raw []byte)error{
 if err:=ctx.Err();err!=nil{return err}
 if q==nil||q.native==nil||q.Metadata==nil||q.Reader.Source==nil||q.Reader.Objects==nil||entry.Contract!="SeriesCorrections"||!validImportRef(entry.Ref)||len(raw)<2||len(raw)>8<<20||rawSHA(raw)!=entry.Ref.SHA256{return v2input.ErrInvalid}
 body:=append([]byte(nil),raw...)
 if err:=q.Metadata.QualifyExportSeries(ctx,entry,append([]byte(nil),body...));err!=nil{return err}
 before,err:=q.Reader.Source.LookupExportSeriesOrigin(ctx,entry,append([]byte(nil),body...));if err!=nil{return err}
 bundle,err:=q.Reader.ReadExportSeriesOrigin(ctx,entry,append([]byte(nil),body...));if err!=nil{return err}
 bundleEntry:=v2input.Entry{Contract:"SeriesOrigin",Ref:v2input.Ref{ArtifactID:entry.Ref.ArtifactID,SHA256:rawSHA(bundle)}}
 if _,err:=q.native.ExportDocumentChildren(ctx,bundleEntry,bundle);err!=nil{return err}
 if err:=q.Inputs.QualifyExportSeriesInputs(ctx,before,bundle);err!=nil{return err}
 // Reader reauthorizes all objects, not only the correction artifact; compare
 // exact pinned content as well as the producer epoch across native execution.
 finalBundle,err:=q.Reader.ReadExportSeriesOrigin(ctx,entry,append([]byte(nil),body...));if err!=nil{return err}
 if !bytes.Equal(bundle,finalBundle){return v2input.ErrInvalid}
 after,err:=q.Reader.Source.LookupExportSeriesOrigin(ctx,entry,append([]byte(nil),body...));if err!=nil{return err};if after!=before{return v2input.ErrInvalid}
 if err:=q.Inputs.QualifyExportSeriesInputs(ctx,after,finalBundle);err!=nil{return err}
 if err:=q.Metadata.QualifyExportSeries(ctx,entry,append([]byte(nil),body...));err!=nil{return err};return ctx.Err()
}
var _ ExportSeriesAuthority=(*NativeExportSeriesOriginQualifier)(nil)
