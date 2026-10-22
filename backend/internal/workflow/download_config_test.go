package workflow
import("testing";"strings")
func TestDownloadVolumeEnvironmentBounds(t *testing.T){
 defaults,err:=DownloadVolumeLimitsFromEnvironment(func(string)string{return ""});if err!=nil||defaults.Bytes!=DefaultVolumeByteLimit||defaults.Files!=MaxDownloadItems{t.Fatal(defaults,err)}
 for _,name:=range []string{"PHOTO_DOWNLOAD_VOLUME_BYTES","PHOTO_DOWNLOAD_VOLUME_FILES"}{for _,raw:=range []string{"0","-1","garbage","999999999999999999999999"}{if _,err:=DownloadVolumeLimitsFromEnvironment(func(key string)string{if key==name{return raw};return ""});err==nil{t.Fatal(name,raw)}}}
 limits,err:=DownloadVolumeLimitsFromEnvironment(func(key string)string{if key=="PHOTO_DOWNLOAD_VOLUME_BYTES"{return "10"};return "1"});if err!=nil{t.Fatal(err)}
 plan,err:=PlanDownloadVolumesWithLimits([]DownloadSource{downloadSource(0,1,"a.jpg",strings.Repeat("a",64)),downloadSource(1,1,"b.jpg",strings.Repeat("b",64))},limits.Bytes,limits.Files)
 if err!=nil||len(plan)!=2||plan[0].Volume!=1||plan[1].Volume!=2{t.Fatal(plan,err)}
}
