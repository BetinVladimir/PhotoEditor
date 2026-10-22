package exportjob

import "testing"

func TestRequiredExportBatchDoesNotWaitForeverOrAcceptPartialSuccess(t *testing.T) {
 for _,status:=range []string{"failed","cancelled","completed_with_errors"} {
  if got:=deliveryBatchChildOutcome(status);got!="failed" {t.Fatalf("terminal required-preset child %q became %q",status,got)}
 }
 if got:=deliveryBatchChildOutcome("completed");got!="complete" {t.Fatal(got)}
 for _,status:=range []string{"queued","running","blocked","paused","cancelling","waiting_review","unavailable","future_status"} {
  if got:=deliveryBatchChildOutcome(status);got!="pending" {t.Fatalf("unconfirmed completion %q became %q",status,got)}
 }
}
