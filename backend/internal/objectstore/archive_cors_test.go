package objectstore

import (
 "testing"

 "github.com/minio/minio-go/v7/pkg/cors"
)

func TestArchiveCORSOriginsAreExactAndDeduplicated(t *testing.T){
 origins,err:=archiveCORSOrigins([]string{"https://app.example","https://app.example","https://portal.example"},false)
 if err!=nil||len(origins)!=2||origins[0]!="https://app.example"||origins[1]!="https://portal.example"{t.Fatal(origins,err)}
 for _,value:=range []string{"*","http://app.example","https://app.example/path","https://user@app.example","https://app.example?x=1","https://app.example?","https://app.example#","https://app.example\n"}{
  if _,err:=archiveCORSOrigins([]string{value},false);err!=ErrInvalid{t.Fatal("accepted invalid production origin",value,err)}
 }
 local,err:=archiveCORSOrigins([]string{"http://127.0.0.2:8080"},true)
 if err!=nil||len(local)!=1{t.Fatal("loopback development origin rejected",local,err)}
}

func TestArchiveCORSReadbackRequiresExactOriginsAndETag(t *testing.T){
 origins:=[]string{"https://app.example","https://portal.example"}
 rule:=cors.Rule{AllowedOrigin:append([]string(nil),origins...),AllowedMethod:[]string{"GET","HEAD","PUT"},AllowedHeader:[]string{"*"},ExposeHeader:[]string{"ETag"}}
 if !archiveCORSInstalled(cors.NewConfig([]cors.Rule{rule}),origins){t.Fatal("exact CORS configuration rejected")}
 rule.AllowedOrigin=append(rule.AllowedOrigin,"*")
 if archiveCORSInstalled(cors.NewConfig([]cors.Rule{rule}),origins){t.Fatal("wildcard origin accepted")}
 rule.AllowedOrigin=origins;rule.ExposeHeader=nil
 if archiveCORSInstalled(cors.NewConfig([]cors.Rule{rule}),origins){t.Fatal("hidden ETag accepted")}
 rule.ExposeHeader=[]string{"ETag"};rule.AllowedMethod=append(rule.AllowedMethod,"DELETE")
 if archiveCORSInstalled(cors.NewConfig([]cors.Rule{rule}),origins){t.Fatal("extra method accepted")}
}
