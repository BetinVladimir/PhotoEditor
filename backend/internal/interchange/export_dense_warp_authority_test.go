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

type exportWarpAuthorityStub struct{calls,index int;parent,field v2input.Entry;cause error}
func(s *exportWarpAuthorityStub)QualifyExportDenseWarpOccurrence(ctx context.Context,parent v2input.Entry,raw []byte,index int,field v2input.Entry)error{
 s.calls++;s.index=index;s.parent=parent;s.field=field
 if err:=validateExportDenseWarpOccurrence(parent,raw,index,field);err!=nil{return err}
 if s.cause!=nil{return s.cause};return ctx.Err()
}
func TestExportDenseWarpExactOccurrenceAndCurrentAuthority(t *testing.T){
 for _,mode:=range []string{"valid","denied","index","foreign","hash","contract","cancelled"}{t.Run(mode,func(t *testing.T){
  _,_,raw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(raw);if err!=nil{t.Fatal(err)}
  field:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("field"))},Contract:"DenseWarpField"}
  steps:=[]any{map[string]any{"kind":"dense_warp","from_canvas":image.Canvas,"to_canvas":image.Canvas,"invertibility":"partial","valid_region":map[string]any{"x":0,"y":0,"width":image.Canvas.Width,"height":image.Canvas.Height},"inverse_field":review.ArtifactRef{ID:field.Ref.ArtifactID,SHA256:field.Ref.SHA256}}}
  hash,err:=canonical.Hash(map[string]any{"steps":steps});if err!=nil{t.Fatal(err)}
  image.TransformChain,err=canonical.Marshal(map[string]any{"steps":steps,"hash":hash});if err!=nil{t.Fatal(err)}
  raw,err=canonical.Marshal(image);if err!=nil{t.Fatal(err)}
  parent:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"WorkingImage"}
  reader:=&exportMaskNodeStub{node:ExportClosureNode{Entry:parent,Body:raw,Children:[]v2input.Entry{field}}}
  source,err:=NewExportTypedArrayGeometrySource(reader,[]v2input.Entry{parent});if err!=nil{t.Fatal(err)}
  authority:=&exportWarpAuthorityStub{};source.warpAuthority=authority
  cause:=errors.New("warp permission revoked");ctx:=context.Background()
  switch mode{case "denied":authority.cause=cause;case "foreign":field.Ref.ArtifactID=uuid.New();case "hash":raw=[]byte("{}");case "contract":field.Contract="WorkingPixels";case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  if mode=="index"||mode=="hash"||mode=="contract"{
   index:=0;if mode=="index"{index=1}
   if err:=validateExportDenseWarpOccurrence(parent,raw,index,field);err==nil{t.Fatal("invalid occurrence accepted")};return
  }
  _,err=source.ResolveExportArrayGeometry(ctx,field)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected warp result",err)}
  if mode=="valid"||mode=="denied"{if authority.calls!=1||authority.index!=0||authority.parent!=parent||authority.field!=field{t.Fatal("exact occurrence lost")}}
  if mode=="denied"&&!errors.Is(err,cause){t.Fatal("authority refusal lost")}
  if mode=="foreign"||mode=="cancelled"{if authority.calls!=0{t.Fatal("unrelated occurrence authorized")}}
 })}
}
func TestSQLExportDenseWarpRejectsCancelledAndUnboundInputs(t *testing.T){
 source:=SQLExportDocumentCatalog{}
 if err:=source.QualifyExportDenseWarpOccurrence(context.Background(),v2input.Entry{},nil,0,v2input.Entry{});!errors.Is(err,v2input.ErrInvalid){t.Fatal(err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=source.QualifyExportDenseWarpOccurrence(ctx,v2input.Entry{},nil,0,v2input.Entry{});!errors.Is(err,context.Canceled){t.Fatal(err)}
}

func TestResolvedSourceWarpUsesCanonicalOccurrenceWithoutSyntheticMaskParent(t *testing.T){
 for _,mode:=range []string{"valid","denied","missing","edge","relabel"}{t.Run(mode,func(t *testing.T){
  _,_,raw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(raw);if err!=nil{t.Fatal(err)}
  field:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("field"))},Contract:"DenseWarpField"}
  steps:=[]any{map[string]any{"kind":"dense_warp","from_canvas":image.Canvas,"to_canvas":image.Canvas,"invertibility":"partial","valid_region":map[string]any{"x":0,"y":0,"width":image.Canvas.Width,"height":image.Canvas.Height},"inverse_field":review.ArtifactRef{ID:field.Ref.ArtifactID,SHA256:field.Ref.SHA256}}}
  hash,err:=canonical.Hash(map[string]any{"steps":steps});if err!=nil{t.Fatal(err)};image.TransformChain,err=canonical.Marshal(map[string]any{"steps":steps,"hash":hash});if err!=nil{t.Fatal(err)}
  raw,err=canonical.Marshal(image);if err!=nil{t.Fatal(err)}
  parent:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"WorkingImage"}
  node:=ExportClosureNode{Entry:parent,Body:raw,Children:[]v2input.Entry{field}}
  authority:=&exportWarpAuthorityStub{};var policy ExportDenseWarpOccurrenceAuthority=authority
  switch mode{case "denied":authority.cause=v2input.ErrInvalid;case "missing":policy=nil;case "edge":node.Children=nil;case "relabel":node.Children[0].Contract="MaskPixels"}
  err=qualifyExportResolvedSourceWarps(context.Background(),node,policy)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected resolved warp authority",mode,err)}
  if mode=="valid"||mode=="denied"{if authority.calls!=1||authority.parent!=parent||authority.field!=field||authority.index!=0{t.Fatal("canonical occurrence lost")}}
 })}
}
