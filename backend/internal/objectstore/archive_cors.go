package objectstore

import (
 "net"
 "net/url"
 "strings"

 "github.com/minio/minio-go/v7/pkg/cors"
)

// Browser multipart PUT must expose ETag; only configured application origins
// may read that receipt. Missing optional origins are ignored for local callers.
func archiveCORSOrigins(values []string,development bool)([]string,error){
 result:=make([]string,0,len(values));seen:=make(map[string]bool,len(values))
 for _,value:=range values{
  if value==""{continue}
  parsed,err:=url.Parse(value)
  if err!=nil||parsed.Host==""||parsed.User!=nil||parsed.Path!=""||parsed.RawQuery!=""||parsed.ForceQuery||parsed.Fragment!=""||parsed.Opaque!=""||parsed.Hostname()==""||strings.ContainsAny(value,"*?#\r\n\x00"){return nil,ErrInvalid}
  if parsed.Scheme!="https"{
   ip:=net.ParseIP(parsed.Hostname())
   if parsed.Scheme!="http"||!development||(parsed.Hostname()!="localhost"&&(ip==nil||!ip.IsLoopback())){return nil,ErrInvalid}
  }
  if parsed.String()!=value{return nil,ErrInvalid}
  if seen[value]{continue}
  seen[value]=true;result=append(result,value)
 }
 return result,nil
}

func archiveCORSInstalled(config *cors.Config,origins []string)bool{
 if config==nil||len(origins)==0||len(config.CORSRules)!=1{return false}
 expected:=make(map[string]bool,len(origins));for _,origin:=range origins{expected[origin]=true}
 rule:=config.CORSRules[0]
 if len(rule.AllowedOrigin)!=len(expected){return false}
 for _,origin:=range rule.AllowedOrigin{if !expected[origin]{return false};delete(expected,origin)}
 methods:=make(map[string]bool,len(rule.AllowedMethod));for _,method:=range rule.AllowedMethod{methods[method]=true}
 return len(expected)==0&&len(rule.AllowedMethod)==3&&len(methods)==3&&methods["PUT"]&&methods["GET"]&&methods["HEAD"]&&
  len(rule.AllowedHeader)==1&&rule.AllowedHeader[0]=="*"&&len(rule.ExposeHeader)==1&&strings.EqualFold(rule.ExposeHeader[0],"ETag")
}
