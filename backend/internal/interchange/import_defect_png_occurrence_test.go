package interchange
import (
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
func TestImportDefectPNGOccurrenceBindsIdentityAndDistinctReportWitnesses(t *testing.T){
 ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("content"))}}
 for _,mode:=range []string{"valid","missing_report","duplicate","wrong_identity","array_scope","foreign_sha"}{t.Run(mode,func(t *testing.T){
  original,report:=ref(),ref();scope:=ImportPreparedScope{Workflow:uuid.New(),Entry:uuid.New(),Contract:"DefectLabelPNG",DefectPNG:&ImportDefectPNGOccurrence{Original:original,Reports:[]v2input.Ref{report}}}
  mapped:=v2input.Ref{ArtifactID:uuid.NewSHA1(scope.Workflow,[]byte("interchange:dependency:"+scope.Entry.String()+":DefectLabelPNG:"+original.ArtifactID.String())),SHA256:original.SHA256}
  switch mode{case "missing_report":scope.DefectPNG.Reports=nil;case "duplicate":scope.DefectPNG.Reports=append(scope.DefectPNG.Reports,report);case "wrong_identity":mapped.ArtifactID=uuid.New();case "array_scope":scope.Contract="DefectLabelMap";case "foreign_sha":mapped.SHA256=rawSHA([]byte("foreign"))}
  err:=validateImportDefectPNGOccurrence(scope,mapped)
  if mode=="valid"{if err!=nil{t.Fatal(err)}}else if err==nil{t.Fatal("unbound PNG occurrence accepted")}
 })}
}
