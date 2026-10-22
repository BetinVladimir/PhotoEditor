package workflow

import (
 "fmt"
 "strconv"
)

type DownloadVolumeLimits struct {Bytes int64;Files int}
func DownloadVolumeLimitsFromEnvironment(getenv func(string)string)(DownloadVolumeLimits,error){
 limits:=DownloadVolumeLimits{Bytes:DefaultVolumeByteLimit,Files:MaxDownloadItems}
 if getenv==nil{return limits,fmt.Errorf("download environment reader required")}
 for _,setting:=range []struct{name string;max int64;target *int64}{
  {"PHOTO_DOWNLOAD_VOLUME_BYTES",DefaultVolumeByteLimit,&limits.Bytes},
 }{if raw:=getenv(setting.name);raw!=""{value,err:=strconv.ParseInt(raw,10,64);if err!=nil||value<1||value>setting.max{return limits,fmt.Errorf("%s outside supported bounds",setting.name)};*setting.target=value}}
 if raw:=getenv("PHOTO_DOWNLOAD_VOLUME_FILES");raw!=""{value,err:=strconv.ParseInt(raw,10,32);if err!=nil||value<1||value>MaxDownloadItems{return limits,fmt.Errorf("PHOTO_DOWNLOAD_VOLUME_FILES outside supported bounds")};limits.Files=int(value)}
 return limits,nil
}
