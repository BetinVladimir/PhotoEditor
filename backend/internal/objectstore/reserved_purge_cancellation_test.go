package objectstore

import (
 "context"
 "errors"
 "testing"
)

func TestReservedPurgeCancellationPrecedesObjectIO(t *testing.T) {
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 // No client is installed: entering physical IO would panic. Cancellation must
 // win even when there is nothing to enumerate or no callback can deliver it.
 var store *Store
 if err:=store.purgeReservedKey(ctx,"reserved-key");!errors.Is(err,context.Canceled){t.Fatalf("lost cancellation: %v",err)}
}
func TestReservedPurgeRejectsMissingStoreWithoutIO(t *testing.T) {
 var store *Store
 if err:=store.purgeReservedKey(context.Background(),"reserved-key");!errors.Is(err,ErrInvalid){t.Fatalf("invalid store accepted: %v",err)}
}
