package objectstore

import("strings";"testing")
func TestImmutableArchiveVersionRejectsControlAndMalformedReceipts(t *testing.T){
 for _,version:=range []string{"","null","v\x00id","v\tid","v\nid","v\rid","v\u0085id",string([]byte{0xff}),strings.Repeat("x",1025)}{if ValidInterchangeVersionID(version){t.Fatal("invalid version admitted")}}
 for _,version:=range []string{"v1","immutable/version+receipt=",strings.Repeat("x",1024)}{if !ValidInterchangeVersionID(version){t.Fatal("valid version refused")}}
}
