package objectstore
import("strings";"testing")
func TestInterchangeMultipartIDEnvelope(t *testing.T){
 for _,id:=range []string{"multipart-1+/=",strings.Repeat("a",1024)}{if !ValidInterchangeMultipartID(id){t.Fatal("valid id rejected")}}
 for _,id:=range []string{"",strings.Repeat("a",1025),"a\x00b","a\tb","a\rb","a\nb","a\x7fb","a\u0085b",string([]byte{0xff})}{if ValidInterchangeMultipartID(id){t.Fatal("invalid id accepted")}}
}
