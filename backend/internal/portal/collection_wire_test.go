package portal
import("encoding/json";"testing")
func TestCollectionWireDistinctNameAndDescription(t *testing.T){
 body,e:=json.Marshal(Collection{Name:"Wedding",Description:"Selected photographs"});if e!=nil{t.Fatal(e)}
 var fields map[string]json.RawMessage;if e=json.Unmarshal(body,&fields);e!=nil{t.Fatal(e)}
 if string(fields["name"])!=`"Wedding"`||string(fields["description"])!=`"Selected photographs"`{t.Fatalf("lost collection text: %s",body)}
}
