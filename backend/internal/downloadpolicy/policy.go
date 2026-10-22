package downloadpolicy

import (
 "fmt"
 "strconv"
 "time"
)

const MaxRetentionSeconds=86400
const MaxLinkSeconds=60
type Policy struct{RetentionSeconds int `json:"retention_seconds"`;LinkSeconds int `json:"link_seconds"`}
func Default()Policy{return Policy{MaxRetentionSeconds,MaxLinkSeconds}}
func(p Policy)Valid()bool{return p.RetentionSeconds>=1&&p.RetentionSeconds<=MaxRetentionSeconds&&p.LinkSeconds>=1&&p.LinkSeconds<=MaxLinkSeconds}
func(p Policy)LinkTTL()time.Duration{return time.Duration(p.LinkSeconds)*time.Second}
func FromEnvironment(getenv func(string)string)(Policy,error){
 p:=Default();if getenv==nil{return p,fmt.Errorf("download policy environment reader required")}
 for _,setting:=range []struct{name string;target *int;maximum int}{{"PHOTO_DOWNLOAD_RETENTION_SECONDS",&p.RetentionSeconds,MaxRetentionSeconds},{"PHOTO_DOWNLOAD_LINK_TTL_SECONDS",&p.LinkSeconds,MaxLinkSeconds}}{
  raw:=getenv(setting.name);if raw==""{continue};value,err:=strconv.Atoi(raw);if err!=nil||value<1||value>setting.maximum||strconv.Itoa(value)!=raw{return Policy{},fmt.Errorf("%s outside supported bounds",setting.name)};*setting.target=value
 };return p,nil
}
