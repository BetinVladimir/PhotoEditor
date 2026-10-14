package delivery

import (
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)
func TestMetadataChildConfigFrozenSourceAndApproval(t *testing.T){
 plan,lease,_:=expansionPlanFixture(t);page,err:=plan.ExportChildren(lease.Run,0,1);if err!=nil{t.Fatal(err)}
 request,_,err:=plan.ExportRequest(lease.Run,page.Items[0]);if err!=nil{t.Fatal(err)}
 repository,_:=sourceStorageFixture(t)
 var config map[string]any
 if json.Unmarshal([]byte(request.ConfigJCS),&config)!=nil{t.Fatal("config")}
 config["metadata_policy"]="copyright";configRaw,err:=canonical.Marshal(config);if err!=nil{t.Fatal(err)}
 request.ConfigJCS=string(configRaw);request.Preset.PresetSHA=digest(configRaw);request.Preset.Config.SHA256=digest(configRaw)
 scope:=repository.scope;scope.Binding=request.Binding
 publication,err:=CompileMetadataSourcePublication(scope,repository.plan.Ref(),repository.plan.SourceJCS(),"copyright",nil);if err!=nil{t.Fatal(err)}
 exportID:=uuid.New()
 raw,pin,err:=CompileMetadataChildConfig(scope.Project,request,exportID,publication,"copyright",nil)
 if err!=nil||digest(raw)!=pin{t.Fatal("metadata child freeze",err)}
 var frozen map[string]json.RawMessage
 if json.Unmarshal(raw,&frozen)!=nil||len(frozen)!=8||string(frozen["v30_metadata_approved_fields"])!="[]"{t.Fatal("missing frozen authority")}
 // Watermark presentation must preserve the same frozen metadata publication.
 _,watermarkRaw,_:=watermarkPinFixture(t)
 var watermarkConfig map[string]json.RawMessage
 if json.Unmarshal(watermarkRaw,&watermarkConfig)!=nil{t.Fatal("watermark config")}
 var mark any
 if json.Unmarshal(watermarkConfig["watermark"],&mark)!=nil{t.Fatal("watermark fixture")}
 config["watermark"]=mark
 markedRaw,err:=canonical.Marshal(config);if err!=nil{t.Fatal(err)}
 marked:=request;marked.ConfigJCS=string(markedRaw);marked.Preset.PresetSHA=digest(markedRaw);marked.Preset.Config.SHA256=marked.Preset.PresetSHA
 markedChild,_,err:=CompileMetadataChildConfig(scope.Project,marked,exportID,publication,"copyright",nil)
 if err!=nil{t.Fatal("watermark metadata child",err)}
 var markedConfig MetadataChildConfig
 if json.Unmarshal(markedChild,&markedConfig)!=nil||markedConfig.Metadata!=publication.Ref()||markedConfig.Request!=marked{t.Fatal("watermark metadata source changed")}
 for _,mode:=range []string{"project","child","binding","preset","policy","approval","export"}{
  current:=request;project:=scope.Project;id:=exportID;policy:="copyright";var approved []string
  switch mode{case "project":project=uuid.New();case "child":current.ChildRunID=uuid.New();case "binding":current.Binding.AssetVersionID=uuid.New();case "preset":current.Preset.Config.SHA256="bad";case "policy":policy="approved_exif";case "approval":approved=[]string{"gps"};case "export":id=uuid.Nil}
  if _,_,err:=CompileMetadataChildConfig(project,current,id,publication,policy,approved);err==nil{t.Fatal(mode)}
 }
}
