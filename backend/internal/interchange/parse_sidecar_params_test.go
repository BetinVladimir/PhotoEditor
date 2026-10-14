package interchange

import (
 "bytes"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/auth"
)

func TestCompileParseParamsPreservesSidecarFormat(t *testing.T) {
 for _,format:=range []string{"photoeditor_bundle","xmp","captureone_sidecar"} {
  t.Run(format,func(t *testing.T){
   params:=ParseParams{SchemaVersion:1,Request:ImportRequest{ProjectID:uuid.New(),Direction:"import",Format:format,
    Bundle:v2input.Ref{ArtifactID:uuid.New(),SHA256:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},MaxExpandedBytes:1024,MaxFiles:10}
   first,err:=CompileParseParams(params);if err!=nil{t.Fatal(err)}
   second,err:=CompileParseParams(params);if err!=nil||!bytes.Equal(first,second){t.Fatal("unstable params",err)}
   if !bytes.Contains(first,[]byte(`"format":"`+format+`"`)){t.Fatal("format was substituted")}
  })
 }
}
func TestCompileParseParamsRefusesUnknownFormats(t *testing.T) {
 for _,format:=range []string{"","XMP","cos","psd","xmp "} {
  params:=ParseParams{SchemaVersion:1,Request:ImportRequest{ProjectID:uuid.New(),Direction:"import",Format:format,
   Bundle:v2input.Ref{ArtifactID:uuid.New(),SHA256:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},MaxExpandedBytes:1024,MaxFiles:10}
  if _,err:=CompileParseParams(params);err==nil{t.Fatalf("accepted %q",format)}
 }
}

func TestSidecarPendingRecoveryRetainsFormatAndPins(t *testing.T) {
 for _,format:=range []string{"xmp","captureone_sidecar"} {
  t.Run(format,func(t *testing.T){
   tenant:=uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
   ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
   request:=CreateRequest{ProjectID:uuid.New(),Direction:"import",Format:format,Bundle:&ref}
   limits:=ParseLimits{RAMBytes:2<<30,ScratchBytes:1<<30,OutputBytes:1<<30,ExpandedBytes:1024,Files:10}
   original,err:=CompileParsePlan(tenant,who,request,"sidecar",BundleEvidence{ref,100},limits);if err!=nil{t.Fatal(err)}
   raw,err:=EncodeParsePending(tenant,who,request,"sidecar",BundleEvidence{ref,100},limits);if err!=nil{t.Fatal(err)}
   restored,err:=DecodeParsePending(tenant,who,request,"sidecar",raw);if err!=nil{t.Fatal(err)}
   params,pin,body:=restored.Params();_,expectedPin,expectedBody:=original.Params()
   if params.Request.Format!=format||pin!=expectedPin||!bytes.Equal(body,expectedBody)||restored.Identity()!=original.Identity(){t.Fatal("recovery changed source format or pins")}
   changed:=request;changed.Format="photoeditor_bundle"
   if _,err:=DecodeParsePending(tenant,who,changed,"sidecar",raw);err==nil{t.Fatal("recovered sidecar under different format")}
  })
 }
}
