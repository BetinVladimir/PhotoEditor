package interchange
import("context";"encoding/json";"strings";"testing";"github.com/google/uuid";"photoeditor/backend/internal/auth")
func TestImportReviewIntentStrictContractsAndOwnership(t *testing.T){
 tid,workflow:=uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 raw:=[]byte(`{"expected_revision":"1","report_sha":"`+strings.Repeat("a",64)+`","decisions":[{"entry_id":"`+uuid.New().String()+`","decision":"reject"}]}`)
 intent,err:=FreezeImportReviewIntent(context.Background(),tid,workflow,who,"review-1","decide",raw);if err!=nil{t.Fatal(err)}
 again,err:=FreezeImportReviewIntent(context.Background(),tid,workflow,who,"review-1","decide",raw);if err!=nil||intent.OwnerID()!=again.OwnerID()||intent.RequestSHA()!=again.RequestSHA(){t.Fatal("unstable intent",err)}
 owned:=intent.DocumentBytes();owned[0]='x';if intent.DocumentBytes()[0]!='{'{t.Fatal("document alias")}
 for _,bad:=range []string{strings.Replace(string(raw),`"1"`,`"01"`,1),strings.Replace(string(raw),`"reject"`,`"approved"`,1),strings.Replace(string(raw),`"expected_revision":"1"`,`"expected_revision":"1","expected_revision":"2"`,1),strings.Replace(string(raw),`"decision":"reject"`,`"decision":"reject","decision":"approve"`,1),strings.Replace(string(raw),`"decisions":[`, `"unknown":true,"decisions":[`,1)}{
 var request ImportReviewRequest;if json.Unmarshal([]byte(bad),&request)==nil{t.Fatal("invalid request accepted",bad)}
 }
}
func TestImportReviewCloseRejectsDecisionsAndUnsafeRevision(t *testing.T){
 for _,revision:=range []string{"0","-1","+1","01","9007199254740991","9223372036854775807"}{var request ImportReviewCloseRequest;if json.Unmarshal([]byte(`{"expected_revision":"`+revision+`","report_sha":"`+strings.Repeat("a",64)+`"}`),&request)==nil{t.Fatal("unsafe revision accepted")}}
 var request ImportReviewCloseRequest;if json.Unmarshal([]byte(`{"expected_revision":"1","report_sha":"`+strings.Repeat("a",64)+`","decisions":[]}`),&request)==nil{t.Fatal("close accepted decisions")}
}
