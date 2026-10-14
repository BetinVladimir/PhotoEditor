package v2optical

import (
 "context"
 "encoding/json"
 "sort"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)

func TestAdoptedReplacementDiscoveryBindsExactTopology(t *testing.T){
 expected:=[]negativeReplacementDiscoveryNode{{uuid.New(),uuid.New(),uuid.New()},{uuid.New(),uuid.New(),uuid.New()}}
 sort.Slice(expected,func(i,j int)bool{return expected[i].Original.String()<expected[j].Original.String()})
 for _,mode:=range []string{"valid","empty","subset","task","run","original","duplicate","reverse","unknown","null"}{
  out:=append([]negativeReplacementDiscoveryNode(nil),expected...)
  switch mode{
  case "empty":out=[]negativeReplacementDiscoveryNode{}
  case "subset":out=out[:1]
  case "task":out[0].Task=uuid.New()
  case "run":out[0].Run=uuid.New()
  case "original":out[0].Original=uuid.New()
  case "duplicate":out[1]=out[0]
  case "reverse":out[0],out[1]=out[1],out[0]
  }
  raw,err:=canonical.Marshal(out);if err!=nil{t.Fatal(err)}
  if mode=="unknown"{var extra []map[string]any;if err=json.Unmarshal(raw,&extra);err!=nil{t.Fatal(err)};extra[0]["extra"]=true;raw,err=canonical.Marshal(extra);if err!=nil{t.Fatal(err)}}
  if mode=="null"{raw=[]byte("null")}
  got,err:=decodeNegativeAdoptedReplacementNodes(raw,expected)
  if mode!="valid"&&mode!="empty"&&mode!="subset"{if err==nil{t.Fatal("forged discovery accepted",mode)};continue}
  if err!=nil||len(got)!=len(out){t.Fatal("valid discovery refused",mode,err)}
  if len(got)>0{got[0].Task=uuid.New();again,err:=decodeNegativeAdoptedReplacementNodes(raw,expected);if err!=nil||again[0].Task!=expected[0].Task{t.Fatal("mutable discovery bytes",err)}}
 }
}

func TestAdoptedReplacementLoaderRequiresReaders(t *testing.T){
 var prepared PreparedNegativeReplacementInputs;db:=&negativeDraftStoreDB{}
 if _,_,err:=prepared.LoadAdoptedReplacementParents(context.Background(),db,nil);err==nil||db.begins!=0{t.Fatal("missing readers reached SQL",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,_,err:=prepared.LoadAdoptedReplacementParents(ctx,db,nil);err!=context.Canceled{t.Fatal("cancellation",err)}
}
