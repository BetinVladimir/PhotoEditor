package interchange

import (
 "context"
 "testing"
 "photoeditor/backend/internal/v2input"
)

type conservativeSequenceAuthority struct{calls *[]string;name string;deny bool}
func(a conservativeSequenceAuthority)QualifyImportedLayerOperation(_ context.Context,_,_ string,raw []byte,_ v2input.SessionVersionBinding)error{
 *a.calls=append(*a.calls,a.name)
 if len(raw)>0{raw[0]='!'}
 if a.deny{return v2input.ErrInvalid};return nil
}
func TestConservativeQualificationSurroundsIndependentSemanticAuthority(t *testing.T){
 for _,mode:=range []string{"valid","runtime_denied","semantic_denied","missing","cancelled"}{t.Run(mode,func(t *testing.T){
  calls:=[]string{}
  q:=importConservativeQualifiedOperation{Runtime:conservativeSequenceAuthority{&calls,"runtime",mode=="runtime_denied"},Semantic:conservativeSequenceAuthority{&calls,"semantic",mode=="semantic_denied"}}
  if mode=="missing"{q.Semantic=nil}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  raw:=[]byte("{}")
  err:=q.QualifyImportedLayerOperation(ctx,"defects.auto_repair","1",raw,v2input.SessionVersionBinding{})
  if string(raw)!="{}"{t.Fatal("authority mutated caller bytes")}
  if mode=="valid"{if err!=nil||len(calls)!=3||calls[0]!="runtime"||calls[1]!="semantic"||calls[2]!="runtime"{t.Fatal(calls,err)}}else if err==nil{t.Fatal("unqualified operation accepted")}
  if (mode=="missing"||mode=="cancelled")&&len(calls)!=0{t.Fatal("invalid request reached authority")}
  if mode=="runtime_denied"&&len(calls)!=1{t.Fatal("semantic called after runtime denial")}
 })}
}
func TestConservativeRuntimeRequiresInstallation(t *testing.T){
 q:=sqlImportConservativeRuntimePolicy{}
 if err:=q.QualifyImportedLayerOperation(context.Background(),"defects.auto_repair","1",nil,v2input.SessionVersionBinding{});err==nil{t.Fatal("uninstalled runtime accepted")}
}
