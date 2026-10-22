package interchange

import (
 "context"
 "encoding/json"
 "os"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

func TestBoundProposalReportRequiresArchiveIdentityAndAcceptedSourcePair(t *testing.T){
 raw,err:=os.ReadFile("../review/testdata/proposal-report-python.json");if err!=nil{t.Fatal(err)}
 var header struct{Params struct{Image v2input.Ref `json:"image"`} `json:"params"`;Protection json.RawMessage `json:"protection"`}
 if json.Unmarshal(raw,&header)!=nil{t.Fatal("report fixture")}
 mask,err:=review.ParseMaskRef(header.Protection);if err!=nil{t.Fatal(err)}
 canvas,err:=canonical.Hash(mask.Canvas);if err!=nil{t.Fatal(err)}
 old:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:header.Params.Image,CanvasHash:canvas}
 target:=old;target.AssetVersionID=uuid.New();target.Source.ArtifactID=uuid.New()
 parsed:=importParsedValidatorFixture()
 archived:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}
 staged:=archived;staged.ArtifactID=uuid.New()
 path:="documents/defect-propose-report.json"
 parsed.Manifest.Inventory=append(parsed.Manifest.Inventory,importBundleInventory{ID:archived.ArtifactID,Path:path,SHA:archived.SHA256,Contract:"DefectProposeReport"})
 parsed.Files=append(parsed.Files,importParsedFile{Path:path,Content:staged,Contract:"DefectProposeReport"})
 parsedRaw,err:=canonical.Marshal(parsed);if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"valid","archive_changed","source_changed","missing_inventory","cancelled"}{t.Run(mode,func(t *testing.T){
  semantic:=&importProposalReportAuthority{}
  ports:=&importRecipePorts{old:old,target:target}
  current:=append([]byte(nil),raw...);if mode=="archive_changed"{current[0]='!'}
  q:=BoundImportDefectProposeReportQualifier{Entry:parsed.Manifest.Entries[0].ID,Old:old,Target:target,Parsed:parsedRaw,Reader:remapArchiveStub{staged.ArtifactID:current},Documents:defectReviewPreparedReader{},Masters:&importMasterReaderStub{},Sources:ports,Candidates:&importDefectCandidatesQualifierStub{},Semantic:semantic}
  if mode=="missing_inventory"{q.Parsed,_=canonical.Marshal(importParsedValidatorFixture())}
  source:=old;if mode=="source_changed"{source.AssetVersionID=uuid.New()}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  err:=q.ValidateArchivedImportDefectProposeReport(ctx,raw,source)
  if mode=="valid"{if err!=nil||semantic.archived!=1{t.Fatal("accepted archive refused",err)}}else if err==nil||semantic.archived!=0{t.Fatal("unbound archive reached semantic authority",err)}
 })}
}
