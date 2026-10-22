package exportjob
import("testing";"github.com/google/uuid";"photoeditor/backend/internal/auth";"photoeditor/backend/internal/canonical";"photoeditor/backend/internal/exportpreset";"photoeditor/backend/internal/v2input")
func groupPin(policy string)PresetPin{quality:=92;cfg:=exportpreset.Config{Format:"jpeg",ICCID:"srgb_v1",Quality:&quality,Subsampling:"444",Alpha:"matte",MatteRGB:[]float64{1,1,1},MetadataPolicy:policy,NamingTemplate:"{original_name}"};sha,_:=canonical.Hash(cfg);return PresetPin{uuid.New(),sha,cfg}}
func TestMixedExportMetadataKeepsPresentationAndExactPins(t *testing.T){
 tid,batch,project,snapshot:=uuid.New(),uuid.New(),uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};pins:=[]PresetPin{groupPin("strip"),groupPin("approved_exif"),groupPin("copyright"),groupPin("strip")};pins[0].Config.Sharpen=&exportpreset.Sharpen{Radius:1,Strength:.2,Threshold:.01};pins[0].SHA,_=canonical.Hash(pins[0].Config)
 groups,err:=compileDeliveryGroups(tid,who,batch,project,snapshot,pins,[]string{"gps"});if err!=nil||len(groups)!=3{t.Fatal(groups,err)}
 if len(groups[0].Request.Policy.RequiredPresets)!=2||groups[0].Request.ExpectedPresetSHA[pins[0].ID]!=pins[0].SHA||!groups[0].Request.Policy.RequireReview||groups[0].Request.Policy.AllowOmitted{t.Fatal("preset/quality controls lost")}
 if len(groups[1].Request.Policy.ApprovedFields)!=1||groups[1].Request.Policy.ApprovedFields[0]!="gps"||len(groups[0].Request.Policy.ApprovedFields)!=0{t.Fatal("consent leaked between groups")}
 replay,err:=compileDeliveryGroups(tid,who,batch,project,snapshot,pins,[]string{"gps"});if err!=nil||replay[1].Key!=groups[1].Key{t.Fatal("unstable child identity",err)}
 pins[0].SHA="a";if _,err:=compileDeliveryGroups(tid,who,batch,project,snapshot,pins,nil);err==nil{t.Fatal("changed config accepted")}
}
func TestExportConsentWithoutApprovedExifPresetRefused(t *testing.T){if _,err:=compileDeliveryGroups(uuid.New(),auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},uuid.New(),uuid.New(),uuid.New(),[]PresetPin{groupPin("strip")},[]string{"gps"});err==nil{t.Fatal("orphan consent accepted")}}
func TestGroupedExportRejectsUnboundedOrExtendedChildReceipt(t *testing.T){ // Child receipts carry only the existing Delivery JobTicket contract.
 if decodeDeliveryTicket([]byte(`{"run_id":"00000000-0000-4000-8000-000000000001","workflow_id":"00000000-0000-4000-8000-000000000002","state":"queued","invented":true}`),new(v2input.JobTicket)){t.Fatal("extended child receipt admitted")}
}
