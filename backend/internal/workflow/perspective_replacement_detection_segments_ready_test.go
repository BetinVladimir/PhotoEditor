package workflow

import (
 "context"
 "testing"
)
func TestReadyReplacementSegmentsRejectsMissingAuthority(t *testing.T){
 r:=ReplacementDetectionDocuments{}
 if _,err:=r.LockReadySegments(context.Background(),nil,ReplacementDetectionSegmentsPlan{});err==nil{t.Fatal("missing transaction accepted")}
 if _,err:=r.VerifyReadySegments(context.Background(),ReplacementDetectionSegmentsPlan{},nil);err==nil{t.Fatal("missing reader accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=r.LockReadySegments(ctx,nil,ReplacementDetectionSegmentsPlan{});err!=context.Canceled{t.Fatal(err)}
 if _,err:=r.VerifyReadySegments(ctx,ReplacementDetectionSegmentsPlan{},nil);err!=context.Canceled{t.Fatal(err)}
}
