package v2optical

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

func TestMultiReplacementReceiptRequiresCurrentProofs(t *testing.T){
 p:=PreparedNegativeReplacementInputs{bucket:"photos"};db:=&negativeDraftStoreDB{}
 got,err:=p.ReadMultiReplacementQueuedReceipt(context.Background(),db,"photos",nil,nil,nil,nil,MultiReplacementDraft{raw:"{}"},uuid.New(),LocatedMultiQualification{},v2qualification.DeploymentLocks{},MultiResourceEstimate{},v2input.Ref{})
 if err==nil||got.Task!=uuid.Nil||db.begins!=0{t.Fatal("unproved receipt read reached SQL",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=p.ReadMultiReplacementQueuedReceipt(ctx,db,"photos",nil,nil,nil,nil,MultiReplacementDraft{},uuid.New(),LocatedMultiQualification{},v2qualification.DeploymentLocks{},MultiResourceEstimate{},v2input.Ref{});!errors.Is(err,context.Canceled){t.Fatal(err)}
}

func TestMultiReplacementReceiptParserRejectsUnscopedPacket(t *testing.T){
 for _,raw:=range [][]byte{nil,[]byte(`{}`),[]byte(`{"schema_version":1}`),make([]byte,65537)}{
  if _,err:=parseMultiReplacementReceipt(context.Background(),multiReplacementDraftSnapshot{},[]byte(`{}`),MultiQualificationPin{},raw,5);err==nil{t.Fatal("unscoped receipt accepted")}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=parseMultiReplacementReceipt(ctx,multiReplacementDraftSnapshot{},nil,MultiQualificationPin{},nil,5);!errors.Is(err,context.Canceled){t.Fatal(err)}
}
