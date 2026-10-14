package interchange

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// Authority must prove current source, resolved producer and exact LUT edge.
// Reader must fully qualify ResolvedStyle, including the native schema probe.
type ExportLUTOccurrenceAuthority interface {
 QualifyExportLUT(context.Context,v2input.Entry,[]byte,v2input.Entry)error
}
type ExportTypedLUTGeometrySource struct {
 reader ExportClosureNodeResolver
 parents []v2input.Entry
 authority ExportLUTOccurrenceAuthority
}
func NewExportTypedLUTGeometrySource(reader ExportClosureNodeResolver, parents []v2input.Entry, authority ExportLUTOccurrenceAuthority)(*ExportTypedLUTGeometrySource,error) {
 if reader==nil||authority==nil||len(parents)<1||len(parents)>v2input.MaxEntries {return nil,v2input.ErrInvalid}
 owned:=append([]v2input.Entry(nil),parents...);seen:=map[uuid.UUID]bool{}
 for _,parent:=range owned {
  if parent.Contract!="ResolvedStyle"||!validImportRef(parent.Ref)||seen[parent.Ref.ArtifactID] {return nil,v2input.ErrInvalid}
  seen[parent.Ref.ArtifactID]=true
 }
 return &ExportTypedLUTGeometrySource{reader:reader,parents:owned,authority:authority},nil
}
func(s *ExportTypedLUTGeometrySource)ResolveExportArrayGeometry(ctx context.Context,entry v2input.Entry)(ExportArrayGeometry,error) {
 var result ExportArrayGeometry
 if err:=ctx.Err();err!=nil {return result,err}
 if s==nil||s.reader==nil||s.authority==nil||entry.Contract!="AbsoluteLUTArray"||!validImportRef(entry.Ref) {return result,v2input.ErrInvalid}
 found:=false
 for _,parent:=range s.parents {
  if err:=ctx.Err();err!=nil {return ExportArrayGeometry{},err}
  node,err:=s.reader.ResolveExportNode(ctx,parent);if err!=nil {return ExportArrayGeometry{},err}
  if node.Entry!=parent||rawSHA(node.Body)!=parent.Ref.SHA256 {return ExportArrayGeometry{},v2input.ErrInvalid}
  var body struct {LUT *struct {
   Artifact v2input.Ref `json:"artifact"`
   Size int `json:"size"`
   DType string `json:"dtype"`
   Order string `json:"order"`
   Profile string `json:"profile_id"`
  } `json:"absolute_lut"`}
  if json.Unmarshal(node.Body,&body)!=nil {return ExportArrayGeometry{},v2input.ErrInvalid}
  if body.LUT==nil||body.LUT.Artifact.ArtifactID!=entry.Ref.ArtifactID {continue}
  lut:=body.LUT
  if lut.Artifact!=entry.Ref||lut.DType!="float32"||lut.Order!="RGB_C"||lut.Profile!="work_rec2020_gamma24_v1" {return ExportArrayGeometry{},v2input.ErrInvalid}
  edge:=false
  for _,child:=range node.Children {if child.Ref.ArtifactID==entry.Ref.ArtifactID {if child!=entry {return ExportArrayGeometry{},v2input.ErrInvalid};edge=true}}
  if !edge {return ExportArrayGeometry{},v2input.ErrInvalid}
  if err:=s.authority.QualifyExportLUT(ctx,parent,append([]byte(nil),node.Body...),entry);err!=nil {return ExportArrayGeometry{},err}
  digest,err:=canonical.Hash(lut);if err!=nil {return ExportArrayGeometry{},err}
  geometry:=ExportArrayGeometry{Width:lut.Size,Height:lut.Size,CanvasSHA:digest,DType:lut.DType}
  if _,err:=exportArrayMaximumBytes(entry,geometry);err!=nil {return ExportArrayGeometry{},err}
  if found&&result!=geometry {return ExportArrayGeometry{},v2input.ErrInvalid}
  found=true;result=geometry
 }
 if !found {return ExportArrayGeometry{},v2input.ErrInvalid}
 return result,ctx.Err()
}
var _ ExportArrayGeometrySource=(*ExportTypedLUTGeometrySource)(nil)
