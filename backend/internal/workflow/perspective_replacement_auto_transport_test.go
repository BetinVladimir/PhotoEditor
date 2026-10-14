package workflow

import (
 "context"
 "testing"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/objectstore"
)
func TestAutomaticPerspectiveTransportRejectsMissingDraftBeforeDB(t *testing.T){
 db:=&detectionDocumentDB{}
 repo:=automaticPerspectiveTransportRepository{params:automaticPerspectiveParamsRepository{source:ReplacementDetectionDocuments{DB:db}},body:"{}"}
 if _,err:=repo.Reserve(context.Background(),artifactjson.Scope{Purpose:"v2_input_chunk_0"},renderDerivativeBytesSHA([]byte("{}")),2);err==nil{t.Fatal("unproven transport reserved")}
 if _,err:=repo.Complete(context.Background(),artifactjson.Scope{},artifactjson.Reservation{},objectstore.Object{});err==nil{t.Fatal("unproven transport completed")}
 if db.calls!=0{t.Fatal("invalid transport reached database")}
}
