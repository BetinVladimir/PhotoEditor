package delivery

import "testing"

func TestMetadataApprovalClosedOwnedAndSorted(t *testing.T){
 fields:=[]string{"gps","author"}
 owned,err:=normalizeMetadataApproval("approved_exif",fields)
 if err!=nil||len(owned)!=2||owned[0]!="author"||owned[1]!="gps"{t.Fatal(owned,err)}
 fields[0]="copyright"
 if owned[1]!="gps"{t.Fatal("mutable consent")}
 for _,policy:=range []string{"strip","copyright"}{
  if _,err:=normalizeMetadataApproval(policy,[]string{"gps"});err==nil{t.Fatal("consent ignored",policy)}
 }
 for _,fields:=range [][]string{{"gps","gps"},{"unknown"},{"GPS"}}{
  if _,err:=normalizeMetadataApproval("approved_exif",fields);err==nil{t.Fatal("invalid consent accepted",fields)}
 }
 if fields,err:=normalizeMetadataApproval("approved_exif",nil);err!=nil||len(fields)!=0{t.Fatal(fields,err)}
}
