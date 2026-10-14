package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
func TestExportResultPreparationRequiresCompleteInstallationBeforeIO(t *testing.T){
 if _,err:=(ExportResultPreparation{}).Prepare(context.Background(),v2input.Ref{});err==nil{t.Fatal("incomplete installation")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=(ExportResultPreparation{}).Prepare(ctx,v2input.Ref{});err!=context.Canceled{t.Fatal("cancelled prepare",err)}
}
func TestExportAdoptionEvidenceOwnsReceiptsAndPinsIdentity(t *testing.T){
 tid,who,intent,compiled,snapshot,raw:=exportLedgerFixture(t)
 frozen,err:=FreezeExportDocuments(context.Background(),intent,compiled,snapshot,raw);if err!=nil{t.Fatal(err)}
 ref:=func(value string)v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(value))}}
 root,bundle,report:=ref("root"),ref("bundle"),ref("report")
 result:=ValidatedExportResult{root:root,bundle:bundle,report:report,shards:[]v2input.Ref{ref("shard")}}
 archive:=VerifiedExportArchive{bundle:VerifiedExportBundleBytes{ref:bundle,object:objectstore.Object{Key:"output-key",VersionID:"v1",SHA256:bundle.SHA256,Bytes:100}},manifest:[]byte(`{}`),files:[]VerifiedExportZIPFile{{Path:"manifest.json",Bytes:2,SHA256:rawSHA([]byte(`{}`))}}}
 config:=SQLExportOutputCatalog{Tenant:tid,Who:who,Request:intent.Request,Key:"export-key",Bucket:"photos",Frozen:frozen}
 evidence,err:=freezeExportAdoptionEvidence(context.Background(),config,intent,frozen,result,archive,nil)
 if err!=nil||evidence.Root()!=root||evidence.Bundle()!=bundle||evidence.SHA256()!=rawSHA(evidence.Body()){t.Fatal("evidence",err)}
 owned:=evidence.Body();owned[0]='x';archive.manifest[0]='x';result.shards[0]=v2input.Ref{}
 if evidence.SHA256()!=rawSHA(evidence.Body()){t.Fatal("evidence aliases inputs")}
 config.Key="another-key"
 if _,err:=freezeExportAdoptionEvidence(context.Background(),config,intent,frozen,result,archive,nil);err==nil{t.Fatal("wrong request identity accepted")}
}
