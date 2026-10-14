package interchange

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type exportWorkingArrayAuthorityStub struct{parent,array v2input.Entry;calls int;cause error}
func(s *exportWorkingArrayAuthorityStub)QualifyExportWorkingArray(ctx context.Context,parent,array v2input.Entry)error{
 s.calls++;s.parent=parent;s.array=array;if s.cause!=nil{return s.cause};return ctx.Err()
}
func TestExportWorkingArrayGeometryRequiresInstalledProducerAuthority(t *testing.T){
 for _,contract:=range []string{"WorkingPixels","WorkingAlpha"}{
 for _,mode:=range []string{"valid","denied","unrelated","cancelled"}{t.Run(contract+"/"+mode,func(t *testing.T){
  _,_,raw:=importWorkingValidatorFixture(t)
  image,err:=review.ParseWorkingImage(raw);if err!=nil{t.Fatal(err)}
  ref:=review.ArtifactRef{ID:uuid.New(),SHA256:rawSHA([]byte("array"))}
  if contract=="WorkingPixels"{image.Pixels=ref}else{image.Alpha=&ref}
  raw,err=canonical.Marshal(image);if err!=nil{t.Fatal(err)}
  parent:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"WorkingImage"}
  array:=v2input.Entry{Ref:v2input.Ref{ArtifactID:ref.ID,SHA256:ref.SHA256},Contract:contract}
  reader:=&exportMaskNodeStub{node:ExportClosureNode{Entry:parent,Body:raw,Children:[]v2input.Entry{array}}}
  source,err:=NewExportTypedArrayGeometrySource(reader,[]v2input.Entry{parent});if err!=nil{t.Fatal(err)}
  cause:=errors.New("producer revoked");authority:=&exportWorkingArrayAuthorityStub{}
  source.workingAuthority=authority;ctx:=context.Background()
  switch mode{case "denied":authority.cause=cause;case "unrelated":array.Ref.ArtifactID=uuid.New();case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  _,err=source.ResolveExportArrayGeometry(ctx,array)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected authority result",err)}
  if mode=="valid"||mode=="denied"{if authority.calls!=1||authority.parent!=parent||authority.array!=array{t.Fatal("exact occurrence not authorized")}}else if authority.calls!=0{t.Fatal("unrelated/cancelled occurrence authorized")}
  if mode=="denied"&&!errors.Is(err,cause){t.Fatal("authority error lost")}
 })}}
}
func TestSQLExportWorkingArrayRejectsInvalidOccurrenceBeforeIO(t *testing.T){
 source:=SQLExportDocumentCatalog{}
 parent:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("parent"))},Contract:"WorkingImage"}
 array:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("array"))},Contract:"DenseWarpField"}
 if err:=source.QualifyExportWorkingArray(context.Background(),parent,array);!errors.Is(err,v2input.ErrInvalid){t.Fatal(err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=source.QualifyExportWorkingArray(ctx,parent,array);!errors.Is(err,context.Canceled){t.Fatal(err)}
}
