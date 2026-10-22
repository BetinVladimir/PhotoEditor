package collection

import("encoding/json";"testing")

func TestSectionCreateClosedRequiredPositionWire(t *testing.T){
 for _,raw:=range []string{`{"expected_revision":1,"name":"A"}`,`{"expected_revision":1,"name":"A","position":null}`,`{"expected_revision":1,"name":"A","position":0,"position":1}`,`{"expected_revision":1,"name":"A","position":0,"other":1}`,`{"expected_revision":1,"name":"A","position":0} {}`,`null`}{
  before:=SectionCreate{ExpectedRevision:7,Name:"original",Position:2};got:=before
  if json.Unmarshal([]byte(raw),&got)==nil||got!=before{t.Fatal("invalid request accepted or mutated receiver",raw,got)}
 }
 for _,raw:=range []string{`{"expected_revision":1,"name":"A","position":0}`,`{"expected_revision":9223372036854775807,"name":"A","position":9007199254740991}`} {
  var got SectionCreate;if err:=json.Unmarshal([]byte(raw),&got);err!=nil{t.Fatal(raw,err)}
  encoded,err:=json.Marshal(got);if err!=nil||string(encoded)!=raw{t.Fatal("receipt fingerprint changed",string(encoded),err)}
 }
}
