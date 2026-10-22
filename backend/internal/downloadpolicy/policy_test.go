package downloadpolicy

import("testing";"time")
func TestPolicyDefaultsAndBounds(t *testing.T){
 p,e:=FromEnvironment(func(string)string{return ""});if e!=nil||p!=Default()||p.LinkTTL()!=time.Minute{t.Fatal(p,e)}
 for _,name:=range []string{"PHOTO_DOWNLOAD_RETENTION_SECONDS","PHOTO_DOWNLOAD_LINK_TTL_SECONDS"}{
  for _,value:=range []string{"0","-1","+1","01"," 1","1 ","1.5","999999999999999999999"}{
   if _,e:=FromEnvironment(func(k string)string{if k==name{return value};return ""});e==nil{t.Fatalf("accepted %s=%q",name,value)}
  }
 }
 p,e=FromEnvironment(func(k string)string{if k=="PHOTO_DOWNLOAD_RETENTION_SECONDS"{return "3600"};return "15"});if e!=nil||p.RetentionSeconds!=3600||p.LinkSeconds!=15||!p.Valid(){t.Fatal(p,e)}
 for k,v:=range map[string]string{"PHOTO_DOWNLOAD_RETENTION_SECONDS":"86401","PHOTO_DOWNLOAD_LINK_TTL_SECONDS":"61"}{if _,e:=FromEnvironment(func(n string)string{if n==k{return v};return ""});e==nil{t.Fatal("accepted above security cap")}}
}
