package interchange

import (
 "bytes"
 "context"
 "encoding/json"

 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

// Implementations must validate the complete operation-specific Params and
// installed runtime/model locks, including all nested dependencies. A registry
// name/version match alone does not satisfy this contract.
type ImportLayerOperationQualifier interface {
 QualifyImportedLayerOperation(context.Context,string,string,[]byte,v2input.SessionVersionBinding)error
}

type ImportPreparedLayerQualifier struct {
 Documents ImportPreparedDocumentReader
 Masks ImportMaskQualifier
 Operations ImportLayerOperationQualifier
}

func(q ImportPreparedLayerQualifier)QualifyImportedLayers(ctx context.Context,raw []byte,binding v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 if q.Documents==nil||q.Masks==nil||q.Operations==nil{return v2input.ErrInvalid}
 owned:=append([]byte(nil),raw...)
 refs,err:=v2draft.ValidateLayerStack(owned,binding);if err!=nil{return err}
 var stack struct{Layers []struct{Operation string `json:"operation"`;Version string `json:"operation_version"`;Params v2input.Ref `json:"params"`} `json:"layers"`}
 if err:=json.Unmarshal(owned,&stack);err!=nil{return err}
 type pinned struct{entry v2input.Entry;raw []byte}
 pins:=make([]pinned,0,len(refs));cache:=map[v2input.Entry][]byte{}
 var consumed int64
 read:=func(entry v2input.Entry)([]byte,error){
  if body,ok:=cache[entry];ok{return append([]byte(nil),body...),nil}
  if err:=ctx.Err();err!=nil{return nil,err}
  body,err:=q.Documents.ReadPreparedImportDocument(ctx,entry,8<<20);if err!=nil{return nil,err}
  if len(body)<2||len(body)>8<<20||rawSHA(body)!=entry.Ref.SHA256{return nil,v2input.ErrInvalid}
  consumed+=int64(len(body));if consumed>128<<20{return nil,v2input.ErrInvalid}
  owned:=append([]byte(nil),body...);cache[entry]=owned;pins=append(pins,pinned{entry,owned})
  return append([]byte(nil),owned...),nil
 }
 // Every authored layer is checked, including disabled/zero-opacity layers.
 for _,layer:=range stack.Layers{
  body,err:=read(v2input.Entry{Ref:layer.Params,Contract:"Params"});if err!=nil{return err}
  if err:=q.Operations.QualifyImportedLayerOperation(ctx,layer.Operation,layer.Version,body,binding);err!=nil{return err}
 }
 for _,ref:=range refs{
  if ref.Contract!="MaskRef"{continue}
  body,err:=read(v2input.Entry{Ref:ref.Ref,Contract:"MaskRef"});if err!=nil{return err}
  mask,err:=review.ParseMaskRef(body);if err!=nil{return err}
  if err:=q.Masks.QualifyImportedMask(ctx,mask,binding);err!=nil{return err}
 }
 // Recheck earlier dependencies after later qualification may have done IO.
 for _,pin:=range pins{body,err:=q.Documents.ReadPreparedImportDocument(ctx,pin.entry,8<<20);if err!=nil{return err};if !bytes.Equal(body,pin.raw){return v2input.ErrInvalid}}
 return ctx.Err()
}
