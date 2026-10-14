package delivery

import (
 "encoding/json"
 "os"
 "bytes"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

func TestFreezeDeliveryMetadataExactSourceAndOwnership(t *testing.T){
 author:="Author";camera:="Model"
 source:=MetadataDocument{Author:&author,CameraModel:&camera,GPS:&MetadataGPS{Latitude:42,Longitude:23}}
 raw,err:=canonical.Marshal(source);if err!=nil{t.Fatal(err)}
 binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:digest([]byte("image"))},CanvasHash:digest([]byte("canvas"))}
 ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:digest(raw)}
 body,pin,err:=FreezeDeliveryMetadata(binding,ref,raw,"approved_exif",[]string{"gps","author"});if err!=nil||pin!=digest(body){t.Fatal(err)}
 var frozen FrozenMetadata;if json.Unmarshal(body,&frozen)!=nil{t.Fatal("bad wire")}
 if frozen.Projection.CameraModel!=nil||frozen.Projection.GPS==nil||frozen.ApprovedFields[0]!="author"{t.Fatal("projection escaped approval")}
 projected,err:=ProjectDeliveryMetadata(source,"approved_exif",[]string{"gps","author"});if err!=nil{t.Fatal(err)}
 *projected.Author="mutated";projected.GPS.Latitude=0
 if author!="Author"||source.GPS.Latitude!=42{t.Fatal("projection aliases source")}
 if _,_,err=FreezeDeliveryMetadata(binding,ref,append(raw,' '),"copyright",nil);err==nil{t.Fatal("changed bytes accepted")}
 for _,fields:=range [][]string{{"gps","gps"},{"serial_number"}}{if _,_,err=FreezeDeliveryMetadata(binding,ref,raw,"approved_exif",fields);err==nil{t.Fatal("invalid approval accepted")}}
 if _,_,err=FreezeDeliveryMetadata(binding,ref,raw,"copyright",[]string{"gps"});err==nil{t.Fatal("irrelevant approval accepted")}
}


func TestDeliveryMetadataSharedWireCorpus(t *testing.T){
 raw,err:=os.ReadFile("../../../docs/implementation-v2/contracts/delivery-metadata-crosslang.json");if err!=nil{t.Fatal(err)}
 var corpus struct{Cases []struct{Policy string `json:"policy"`;Approved []string `json:"approved_fields"`;Binding v2input.SessionVersionBinding `json:"binding"`;Ref v2input.Ref `json:"metadata_source"`;Source string `json:"source_jcs"`;Frozen string `json:"frozen_jcs"`;SHA string `json:"frozen_sha256"`} `json:"cases"`}
 if json.Unmarshal(raw,&corpus)!=nil||len(corpus.Cases)!=4{t.Fatal("invalid shared corpus")}
 for _,c:=range corpus.Cases{
  body,pin,err:=FreezeDeliveryMetadata(c.Binding,c.Ref,[]byte(c.Source),c.Policy,c.Approved)
  if err!=nil||!bytes.Equal(body,[]byte(c.Frozen))||pin!=c.SHA{t.Fatal("cross-language receipt mismatch",c.Policy,err)}
  if _,err=RestoreDeliveryMetadata(body,pin,c.Binding,c.Ref,[]byte(c.Source),c.Policy,c.Approved);err!=nil{t.Fatal("cannot restore shared receipt",err)}
  changed:=c.Binding;changed.AssetVersionID=uuid.New()
  if _,err=RestoreDeliveryMetadata(body,pin,changed,c.Ref,[]byte(c.Source),c.Policy,c.Approved);err==nil{t.Fatal("foreign version restored")}
  if c.Policy!="strip"{if _,err=RestoreDeliveryMetadata(body,pin,c.Binding,c.Ref,[]byte(c.Source),"strip",nil);err==nil{t.Fatal("receipt chose own policy")}}
 }
}
