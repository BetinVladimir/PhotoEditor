package v2input

import (
 "context"
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)

func TestMetadataClosureExactRefsAndOwnedParams(t *testing.T){
 workflow:=uuid.New();task,_:=TaskID(workflow,"metadata.write",0)
 encoded:=Ref{uuid.New(),"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
 raster:=Ref{uuid.New(),encoded.SHA256}
 projection:=Ref{uuid.NewSHA1(task,[]byte("delivery:metadata:projection:1")),encoded.SHA256}
 raw,_:=canonical.Marshal(map[string]any{"encoded":encoded,"document":projection,"policy":"copyright_only","copy_gps":false,"tool_id":"exiftool_v1"})
 hash,_:=canonical.Hash(json.RawMessage(raw))
 params:=Ref{uuid.NewSHA1(task,[]byte("delivery:metadata:params:1")),hash}
 closure,err:=CompileDeliveryMetadataClosure(workflow,raw,params,encoded,raster,projection)
 if err!=nil||len(closure.Direct)!=4||closure.InputDigest==""{t.Fatal(err)}
 saved:=string(closure.ParamsJCS);raw[0]='x'
 if string(closure.ParamsJCS)!=saved{t.Fatal("borrowed Params bytes")}
 if _,err:=closure.Open(context.Background());err!=nil{t.Fatal(err)}
 if _,err:=CompileDeliveryMetadataClosure(workflow,[]byte(saved),params,encoded,encoded,projection);err==nil{t.Fatal("raster alias accepted")}
 wrong:=projection;wrong.ArtifactID=uuid.New()
 if _,err:=CompileDeliveryMetadataClosure(workflow,[]byte(saved),params,encoded,raster,wrong);err==nil{t.Fatal("projection substitution accepted")}
}
