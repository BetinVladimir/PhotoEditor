package workflow

import (
 "bytes"
 "context"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
)

func TestIngestCaptureTimeSidebandNeverInventsTimezone(t *testing.T){
 source:=review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("a",64)}
 for _,tc:=range []struct{name string;instant,local *string;valid bool}{
  {"historical absent",nil,nil,true},
  {"qualified UTC",captureText("2026-10-17T10:20:30Z"),nil,true},
  {"floating local",nil,captureText("2026-10-17T13:20:30"),true},
  {"invalid calendar",captureText("2026-02-30T10:20:30Z"),nil,false},
  {"unsupported fractional precision",captureText("2026-10-17T10:20:30.1Z"),nil,false},
  {"unsupported zero year",nil,captureText("0000-10-17T10:20:30"),false},
  {"unqualified instant",captureText("2026-10-17T10:20:30"),nil,false},
  {"offset in floating",nil,captureText("2026-10-17T13:20:30+03:00"),false},
  {"ambiguous dual",captureText("2026-10-17T10:20:30Z"),captureText("2026-10-17T13:20:30"),false},
 }{
  t.Run(tc.name,func(t *testing.T){
   d:=inspectedAsset{Source:source,Width:400,Height:300,BitDepth:16,Orientation:1,Format:"jpeg",CapturedAt:tc.instant,CapturedLocal:tc.local}
   raw,err:=canonical.Marshal(d);if err!=nil{t.Fatal(err)}
   got,err:=ReadIngestMetadata(context.Background(),bytes.NewReader(raw),metadataRef(raw),source)
   if (err==nil)!=tc.valid{t.Fatalf("valid=%v err=%v",tc.valid,err)}
   if tc.valid&&((got.CapturedAt==nil)!=(tc.instant==nil)||(got.CapturedLocal==nil)!=(tc.local==nil)){t.Fatalf("time classification changed: %+v",got)}
   if tc.instant==nil&&tc.local==nil&&(bytes.Contains(raw,[]byte("captured_at"))||bytes.Contains(raw,[]byte("captured_local"))){t.Fatal("historical wire changed")}
  })
 }
}
func captureText(v string)*string{return &v}
