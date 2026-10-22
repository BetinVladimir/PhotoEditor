package interchange

import (
 "bytes"
 "compress/zlib"
 "context"
 "encoding/binary"
 "encoding/json"
 "fmt"
 "hash/crc32"
 "io"
 "os"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type defectPNGSourceStub struct{inputs []ExportDefectPNGInput;calls int;change bool}
func(s *defectPNGSourceStub)ResolveExportDefectPNG(context.Context,v2input.Entry)([]ExportDefectPNGInput,error){s.calls++;out:=append([]ExportDefectPNGInput(nil),s.inputs...);if s.change&&s.calls>1&&len(out)>0{out[0].Labels.VersionID="changed"};return out,nil}
type defectPNGObjects struct{bodies map[string][]byte;closed int}
type defectPNGStream struct{*bytes.Reader;owner *defectPNGObjects}
func(s *defectPNGStream)Close()error{s.owner.closed++;return nil}
func(s *defectPNGObjects)Open(_ context.Context,o objectstore.Object)(io.ReadCloser,error){body,ok:=s.bodies[o.Key];if !ok{return nil,v2input.ErrInvalid};return &defectPNGStream{bytes.NewReader(body),s},nil}
func defectPNGFixture(t *testing.T,changed bool)([]byte,[]byte,review.VerifiedProposal){
 t.Helper()
 raw,err:=os.ReadFile("../review/testdata/defect-proposal-python.json");if err!=nil{t.Fatal(err)}
 var fields map[string]json.RawMessage;if json.Unmarshal(raw,&fields)!=nil{t.Fatal("proposal")}
 var canvas struct{Width,Height int};if json.Unmarshal(fields["canvas"],&canvas)!=nil{t.Fatal("canvas")}
 header:=fmt.Sprintf("{'descr': '<u2', 'fortran_order': False, 'shape': (%d, %d), }",canvas.Height,canvas.Width)
 padding:=(16-(10+len(header)+1)%16)%16;header+=strings.Repeat(" ",padding)+"\n"
 labels:=append([]byte{147,'N','U','M','P','Y',1,0},byte(len(header)),byte(len(header)>>8))
 labels=append(labels,[]byte(header)...);labels=append(labels,make([]byte,canvas.Width*canvas.Height*2)...)
 fields["candidates"]=json.RawMessage(`[]`)
 fields["label_map"],err=canonical.Marshal(v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(labels)});if err!=nil{t.Fatal(err)}
 raw,err=canonical.Marshal(fields);if err!=nil{t.Fatal(err)}
 proposal,err:=review.ParseDefectProposalStructure(context.Background(),raw,review.ArtifactRef{ID:uuid.New(),SHA256:rawSHA(raw)});if err!=nil{t.Fatal(err)}
 png:=[]byte{137,80,78,71,13,10,26,10}
 chunk:=func(kind string,data []byte){var size [4]byte;binary.BigEndian.PutUint32(size[:],uint32(len(data)));png=append(png,size[:]...);png=append(png,kind...);png=append(png,data...);sum:=crc32.NewIEEE();_,_=sum.Write([]byte(kind));_,_=sum.Write(data);binary.BigEndian.PutUint32(size[:],sum.Sum32());png=append(png,size[:]...)}
 ihdr:=make([]byte,13);binary.BigEndian.PutUint32(ihdr[:4],uint32(canvas.Width));binary.BigEndian.PutUint32(ihdr[4:8],uint32(canvas.Height));ihdr[8]=16;chunk("IHDR",ihdr)
 var compressed bytes.Buffer;writer:=zlib.NewWriter(&compressed)
 for y:=0;y<canvas.Height;y++{row:=make([]byte,1+canvas.Width*2);if changed&&y==0{row[2]=1};if _,err:=writer.Write(row);err!=nil{t.Fatal(err)}}
 if err:=writer.Close();err!=nil{t.Fatal(err)}
 data:=compressed.Bytes();for len(data)>0{size:=len(data);if size>65536{size=65536};chunk("IDAT",data[:size]);data=data[size:]};chunk("IEND",nil)
 return labels,png,proposal
}
func TestExportDefectPNGRequiresMatchingPhysicalLabelsAndStableAuthority(t *testing.T){
 for _,mode:=range []string{"valid","pixel_mismatch","png_digest","labels_digest","authority_changed","missing_authority","empty_witness","cancelled"}{t.Run(mode,func(t *testing.T){
  labels,png,proposal:=defectPNGFixture(t,mode=="pixel_mismatch")
  labelObject:=objectstore.Object{Key:"labels",VersionID:"l1",SHA256:rawSHA(labels),Bytes:int64(len(labels)),ContentType:"application/octet-stream"}
  object:=objectstore.Object{Key:"png",VersionID:"p1",SHA256:rawSHA(png),Bytes:int64(len(png)),ContentType:"image/png"}
  entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:object.SHA256},Contract:"DefectLabelPNG"}
  source:=&defectPNGSourceStub{inputs:[]ExportDefectPNGInput{{Report:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("report"))},Proposal:proposal,Labels:labelObject}},change:mode=="authority_changed"}
  objects:=&defectPNGObjects{bodies:map[string][]byte{"labels":labels,"png":png}}
  q:=ExportDefectPNGQualifier{Objects:objects,Source:source}
  switch mode{case "png_digest":objects.bodies["png"]=append(append([]byte(nil),png...),0);case "labels_digest":objects.bodies["labels"]=append(append([]byte(nil),labels...),0);case "missing_authority":q.Source=nil;case "empty_witness":source.inputs=nil}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  err:=q.QualifyExportBinary(ctx,entry,object)
  if mode=="valid"{if err!=nil||source.calls!=2||objects.closed!=3{t.Fatal("physical qualification",err,source.calls,objects.closed)}}else if err==nil{t.Fatal("unqualified PNG accepted")}
 })}
}
func TestExportDefectPNGRouteDoesNotUseImageFallback(t *testing.T){
 stub:=&exportBinaryQualifierStub{}
 router:=ExportNativeQualifierRouter{DefectPNGs:stub}
 if err:=router.QualifyExportBinary(context.Background(),v2input.Entry{Contract:"DefectLabelPNG"},objectstore.Object{});err!=nil||stub.calls!=1{t.Fatal("PNG route",err)}
 if err:=(ExportNativeQualifierRouter{Images:stub}).QualifyExportBinary(context.Background(),v2input.Entry{Contract:"DefectLabelPNG"},objectstore.Object{});err==nil{t.Fatal("generic image fallback accepted")}
}

type defectPNGOccurrenceStub struct{calls int}
func(s *defectPNGOccurrenceStub)QualifyExportDefectPNG(context.Context,v2input.Entry,[]byte,v2input.Entry)error{s.calls++;return nil}
func TestTypedDefectPNGSourceRequiresAuthorityAndExactOriginalReport(t *testing.T){
 for _,mode:=range []string{"missing_authority","wrong_parent_hash","wrong_parent_identity","cancelled"}{t.Run(mode,func(t *testing.T){
  parent:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{}`))},Contract:"DefectProposeReport"}
  node:=ExportClosureNode{Entry:parent,Body:[]byte(`{}`)}
  if mode=="wrong_parent_hash"{node.Body=[]byte(`{"changed":true}`)}
  if mode=="wrong_parent_identity"{node.Entry.Ref.ArtifactID=uuid.New()}
  reader:=&exportDefectJSONStub{node:node};authority:=&defectPNGOccurrenceStub{}
  source:=ExportTypedDefectPNGSource{Reader:reader,Catalog:SQLExportDocumentCatalog{},Reports:[]v2input.Entry{parent},Authority:authority}
  if mode=="missing_authority"{source.Authority=nil}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  if result,err:=source.ResolveExportDefectPNG(ctx,v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("png"))},Contract:"DefectLabelPNG"});err==nil||result!=nil{t.Fatal("unverified report accepted")}
  if authority.calls!=0{t.Fatal("authority reached before immutable report identity")}
  if (mode=="missing_authority"||mode=="cancelled")&&reader.calls!=0{t.Fatal("unqualified report IO")}
 })}
}
