package portal

import (
 "encoding/json"
 "testing"
 "github.com/google/uuid"
)

func TestPreparationAcceptedWireOmitsAbandonedCollection(t *testing.T) {
 key,preset,job:=uuid.NewString(),uuid.New(),uuid.New()
 value:=DownloadPreparationResolution{Status:"accepted",SelectionKey:key,StartKey:key,PresetID:preset,Ticket:&DownloadAccepted{DownloadJobID:job,Status:"queued"}}
 body,err:=json.Marshal(value);if err!=nil{t.Fatal(err)}
 var wire map[string]json.RawMessage;if err=json.Unmarshal(body,&wire);err!=nil{t.Fatal(err)}
 if len(wire)!=5||wire["collection_id"]!=nil||wire["ticket"]==nil{t.Fatalf("accepted shape: %s",body)}
 var decoded DownloadPreparationResolution
 if err=decodePortalPreparationReceipt(body,&decoded);err!=nil{t.Fatal(err)}
 if decoded.CollectionID!=nil||decoded.Ticket==nil||decoded.Ticket.DownloadJobID!=job{t.Fatalf("accepted receipt changed: %+v",decoded)}
}

func TestPreparationAbandonedWireIncludesExactCollectionWithoutTicket(t *testing.T) {
 key,preset,collection:=uuid.NewString(),uuid.New(),uuid.New()
 value:=DownloadPreparationResolution{Status:"abandoned",SelectionKey:key,StartKey:key,PresetID:preset,CollectionID:&collection}
 body,err:=json.Marshal(value);if err!=nil{t.Fatal(err)}
 var wire map[string]json.RawMessage;if err=json.Unmarshal(body,&wire);err!=nil{t.Fatal(err)}
 if len(wire)!=5||wire["collection_id"]==nil||wire["ticket"]!=nil{t.Fatalf("abandoned shape: %s",body)}
 var decoded DownloadPreparationResolution
 if err=decodePortalPreparationReceipt(body,&decoded);err!=nil{t.Fatal(err)}
 if decoded.CollectionID==nil||*decoded.CollectionID!=collection||decoded.Ticket!=nil{t.Fatalf("abandoned receipt changed: %+v",decoded)}
}
