package contactsheet

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/idempotency"
)
func TestAbandonReceiptRequiresExactSelectionKeyAndNoResource(t *testing.T){
 request:=CreateRequest{SelectionSnapshotID:uuid.New(),Locale:"en",Columns:4,Rows:5};key:=uuid.NewString();body,err:=abandonedBody(request,key);if err!=nil{t.Fatal(err)}
 out:=idempotency.Response{Status:200,Body:body};if !isAbandoned(out,request,key){t.Fatal("own tombstone not recognized")}
 if isAbandoned(out,request,uuid.NewString()){t.Fatal("substituted key accepted")};changed:=request;changed.SelectionSnapshotID=uuid.New();if isAbandoned(out,changed,key){t.Fatal("substituted selection accepted")}
 resource:=uuid.New();out.ResourceID=&resource;if isAbandoned(out,request,key){t.Fatal("resource-bearing abandonment accepted")}
}
func TestAbandonCancelledBeforeDependencies(t *testing.T){ctx,cancel:=context.WithCancel(context.Background());cancel();_,err:=(Service{}).Abandon(ctx,auth.Identity{},uuid.New(),uuid.NewString(),CreateRequest{});if !errors.Is(err,context.Canceled){t.Fatalf("cancelled admission touched validation/dependencies: %v",err)}}
