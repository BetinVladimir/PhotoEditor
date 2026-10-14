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
type exportMaskNodeStub struct{node ExportClosureNode;err error;calls int}
func(s *exportMaskNodeStub)ResolveExportNode(context.Context,v2input.Entry)(ExportClosureNode,error){s.calls++;return s.node,s.err}

func TestExportTypedMaskGeometryExactDocumentAndChild(t *testing.T){
 for _,mode:=range []string{"valid","array_authority","foreign_array","body_hash","entry","child_missing","child_contract","child_hash_conflict","child_role_conflict","resolver_error","cancelled"}{t.Run(mode,func(t *testing.T){
  _,_,working:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(working);if err!=nil{t.Fatal(err)}
  array:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("array"))}
  chainHash,err:=canonical.Hash(map[string]any{"steps":[]any{}});if err!=nil{t.Fatal(err)}
  chain,err:=canonical.Marshal(map[string]any{"steps":[]any{},"hash":chainHash});if err!=nil{t.Fatal(err)}
  mask:=review.MaskRef{Artifact:review.ArtifactRef{ID:array.ArtifactID,SHA256:array.SHA256},Canvas:image.Canvas,TransformChain:chain,Selector:"manual",Confidence:1,Protected:true}
  raw,err:=canonical.Marshal(mask);if err!=nil{t.Fatal(err)}
  entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"MaskRef"}
  stub:=&exportMaskNodeStub{node:ExportClosureNode{Entry:entry,Body:raw,Children:[]v2input.Entry{{Ref:array,Contract:"MaskPixels"}}}}
  entries:=[]v2input.Entry{entry};source,err:=NewExportTypedMaskGeometrySource(stub,entries);if err!=nil{t.Fatal(err)}
  entries[0].Contract="caller_mutated"
  authority:=&exportMaskArrayAuthorityStub{}
  source.authority=authority
  cause:=errors.New("authority revoked")
  ctx:=context.Background()
  switch mode{case "array_authority":authority.cause=cause;case "foreign_array":array.ArtifactID=uuid.New();case "body_hash":stub.node.Body=[]byte("{}");case "entry":stub.node.Entry.Contract="WorkingImage";case "child_missing":stub.node.Children=nil;case "child_contract":stub.node.Children[0].Contract="WorkingPixels";case "child_hash_conflict":conflict:=stub.node.Children[0];conflict.Ref.SHA256=rawSHA([]byte("foreign mask"));stub.node.Children=append(stub.node.Children,conflict);case "child_role_conflict":conflict:=stub.node.Children[0];conflict.Contract="MaskArray";stub.node.Children=append(stub.node.Children,conflict);case "resolver_error":stub.err=cause;case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  got,err:=source.ResolveExportMaskGeometry(ctx,array)
  if (err==nil)!=(mode=="valid"){t.Fatalf("unexpected geometry result: %v",err)}
  if mode=="valid"{hash,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)};if got.CanvasSHA!=hash||int64(got.Width)!=image.Canvas.Width||int64(got.Height)!=image.Canvas.Height{t.Fatal("geometry substitution")}}
  if (mode=="resolver_error"||mode=="array_authority")&&!errors.Is(err,cause){t.Fatal("authority error lost")}
  if mode=="valid"||mode=="array_authority"{if authority.calls!=1||authority.parent!=entry||authority.array.Ref!=array||authority.array.Contract!="MaskPixels"||rawSHA(authority.raw)!=entry.Ref.SHA256{t.Fatal("mask occurrence authority lost")}}else if authority.calls!=0{t.Fatal("unbound mask reached authority")}
  if mode=="cancelled"&&stub.calls!=0{t.Fatal("cancelled resolution performed IO")}
 })}
}

func TestExportTypedMaskGeometryConstructorRejectsUnscopedInputs(t *testing.T){
 stub:=&exportMaskNodeStub{}
 entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("mask"))},Contract:"MaskRef"}
 conflict:=entry;conflict.Ref.SHA256=rawSHA([]byte("conflicting mask"))
 for _,entries:=range [][]v2input.Entry{nil,{entry,entry},{entry,conflict},{{Ref:entry.Ref,Contract:"WorkingImage"}}}{
  if _,err:=NewExportTypedMaskGeometrySource(stub,entries);err==nil{t.Fatal("invalid mask inventory accepted")}
 }
 if _,err:=NewExportTypedMaskGeometrySource(nil,[]v2input.Entry{entry});err==nil{t.Fatal("missing scoped resolver accepted")}
}


type exportMaskArrayAuthorityStub struct{calls int;parent,array v2input.Entry;raw []byte;cause error}
func(s *exportMaskArrayAuthorityStub)QualifyExportMaskArray(ctx context.Context,parent v2input.Entry,raw []byte,array v2input.Entry)error{
 s.calls++;s.parent=parent;s.array=array;s.raw=append([]byte(nil),raw...)
 if s.cause!=nil{return s.cause};return ctx.Err()
}
func TestSQLExportMaskArrayRejectsInvalidAndCancelledBeforeIO(t *testing.T){
 source:=SQLExportDocumentCatalog{}
 if err:=source.QualifyExportMaskArray(context.Background(),v2input.Entry{},nil,v2input.Entry{});!errors.Is(err,v2input.ErrInvalid){t.Fatal(err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=source.QualifyExportMaskArray(ctx,v2input.Entry{},nil,v2input.Entry{});!errors.Is(err,context.Canceled){t.Fatal(err)}
}


func TestExportMaskGeometryAcceptsParentCountWithinGraphLimit(t *testing.T){
 parents:=make([]v2input.Entry,10001)
 for i:=range parents{parents[i]=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("mask"))},Contract:"MaskRef"}}
 source,err:=NewExportTypedMaskGeometrySource(&exportMaskNodeStub{},parents)
 if err!=nil||len(source.masks)!=len(parents){t.Fatal("valid graph parents truncated",err)}
 parents[0].Contract="WorkingImage"
 if source.masks[0].Contract!="MaskRef"{t.Fatal("parent collection not owned")}
}
