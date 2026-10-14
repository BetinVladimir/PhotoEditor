package v2optical

import (
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestUnchangedSourceLocatorClosedAndBounded(t *testing.T){
 value:=negativeSourceLocator{ArtifactID:uuid.New(),SHA256:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",Bucket:"photos",Key:"original",Version:"v1",Bytes:100,Media:"application/json"}
 ref:=v2input.Ref{ArtifactID:value.ArtifactID,SHA256:value.SHA256};raw,_:=canonical.Marshal(value)
 if _,err:=decodeNegativeUnchangedSource(raw,"photos",ref);err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"id","sha","bucket","version","size","mime"}{t.Run(mode,func(t *testing.T){bad:=value
  switch mode{case "id":bad.ArtifactID=uuid.New();case "sha":bad.SHA256="bad";case "bucket":bad.Bucket="other";case "version":bad.Version="null";case "size":bad.Bytes=65537;case "mime":bad.Media="text/plain"}
  packet,_:=canonical.Marshal(bad);if _,err:=decodeNegativeUnchangedSource(packet,"photos",ref);err==nil{t.Fatal("invalid locator accepted")}
 })}
}
