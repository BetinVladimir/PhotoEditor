package collection

import (
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/idempotency"
)

func TestBulkAbandonFingerprintNilIsNeverValidAdmission(t *testing.T){
 id:=uuid.New();selected:=SelectionRequest{CollectionRevision:"7",Scope:"selected",ItemIDs:[]uuid.UUID{uuid.New()}}
 normalized,revision,err:=normalizeSelection(selected);if err!=nil||revision!=7||len(normalized.ItemIDs)!=1{t.Fatalf("selection normalization: %v",err)}
 valid,_,err:=normalizeBulk(BulkRequest{ExpectedRevision:revision,SelectionSnapshotID:id,Action:"rating",Params:[]byte(`{"rating":5}`)});if err!=nil{t.Fatal(err)}
 tombstone:=valid;tombstone.SelectionSnapshotID=uuid.Nil
 if _,_,err=normalizeBulk(tombstone);err==nil{t.Fatal("nil sentinel admitted as a normal bulk request")}
 acceptedHash,err:=canonical.Hash(valid);if err!=nil{t.Fatal(err)};tombstoneHash,err:=canonical.Hash(tombstone);if err!=nil{t.Fatal(err)}
 if acceptedHash==tombstoneHash{t.Fatal("late accepted snapshot aliases missing-snapshot tombstone")}
}
func TestBulkAbandonedReceiptExactParentAndKey(t *testing.T){
 id:=uuid.New();body,err:=bulkAbandonBody(id,"original-key");if err!=nil{t.Fatal(err)}
 receipt:=idempotency.Response{Status:200,Body:body}
 if !isBulkAbandoned(receipt,id,"original-key"){t.Fatal("exact tombstone refused")}
 if isBulkAbandoned(receipt,uuid.New(),"original-key")||isBulkAbandoned(receipt,id,"another-key"){t.Fatal("cross-parent/key tombstone accepted")}
 resource:=uuid.New();receipt.ResourceID=&resource;if isBulkAbandoned(receipt,id,"original-key"){t.Fatal("accepted resource treated as abandoned")}
 receipt.ResourceID=nil;receipt.Status=202;if isBulkAbandoned(receipt,id,"original-key"){t.Fatal("accepted response treated as abandoned")}
}
