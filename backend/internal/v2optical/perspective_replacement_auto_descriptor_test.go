package v2optical

import (
 "context"
 "testing"
)
func TestAutomaticStageDescriptorRejectsMissingAndCancelledDraft(t *testing.T){
 for _,raw:=range []string{"","{}","null","[]"}{
  if _,err:=(PerspectiveReplacementDraft{raw:raw}).AutomaticStageDescriptor(context.Background(),LocatedPerspectiveQualification{});err==nil{t.Fatal("invalid draft accepted",raw)}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=(PerspectiveReplacementDraft{}).AutomaticStageDescriptor(ctx,LocatedPerspectiveQualification{});err!=context.Canceled{t.Fatal(err)}
}
func TestAutomaticTransportRequiresScopedPublisher(t *testing.T){
 if _,err:=(PerspectiveReplacementDraft{}).BuildAutomaticTransport(context.Background(),LocatedPerspectiveQualification{},nil);err==nil{t.Fatal("missing publisher accepted")}
}
func TestAutomaticQueuedReceiptRequiresOwnedQualifiedDraft(t *testing.T){
 if _,err:=(PerspectiveReplacementDraft{}).ParseAutomaticQueuedReceipt(context.Background(),LocatedPerspectiveQualification{},[]byte("{}"));err==nil{t.Fatal("unproven receipt accepted")}
}
