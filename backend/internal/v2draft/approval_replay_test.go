package v2draft
import("encoding/json";"testing";"github.com/google/uuid")
func TestApprovalReceiptBindsFourFieldsAndCanonicalRevision(t *testing.T){
 stream,document,approval:=uuid.New(),uuid.New(),uuid.New()
 wire:=func(revision string)[]byte{raw,_:=json.Marshal(map[string]any{"id":stream,"revision":revision,"document_id":document,"approval_id":approval});return raw}
 if got,revision,e:=decodeApprovalReceipt(wire("3"),stream,document);e!=nil||got!=approval||revision!=3{t.Fatal(got,revision,e)}
 for _,revision:=range []string{"0","03","+3","9223372036854775808"}{if _,_,e:=decodeApprovalReceipt(wire(revision),stream,document);e==nil{t.Fatal("bad revision accepted",revision)}}
 for _,raw:=range [][]byte{wire("3"),[]byte(`{}`)}{if _,_,e:=decodeApprovalReceipt(raw,uuid.New(),document);e==nil{t.Fatal("foreign stream accepted")}}
 var fields map[string]any;json.Unmarshal(wire("3"),&fields);fields["secret"]="never";raw,_:=json.Marshal(fields);if _,_,e:=decodeApprovalReceipt(raw,stream,document);e==nil{t.Fatal("extra field accepted")}
}
