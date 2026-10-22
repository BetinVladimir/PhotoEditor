package interchange
import (
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
type ImportDefectPNGOccurrence struct{
 Original v2input.Ref `json:"original"`
 Reports []v2input.Ref `json:"reports"`
}
func validateImportDefectPNGOccurrence(scope ImportPreparedScope,ref v2input.Ref)error{
 if scope.DefectPNG==nil{return nil}
 item:=scope.DefectPNG
 if scope.Contract!="DefectLabelPNG"||scope.Workflow==uuid.Nil||scope.Entry==uuid.Nil||scope.Warp!=nil||scope.LUT!=nil||scope.DefectLabel!=nil||scope.Background!=nil||!validImportRef(ref)||!validImportRef(item.Original)||ref.SHA256!=item.Original.SHA256||len(item.Reports)<1||len(item.Reports)>128{return v2input.ErrInvalid}
 if ref.ArtifactID!=uuid.NewSHA1(scope.Workflow,[]byte("interchange:dependency:"+scope.Entry.String()+":DefectLabelPNG:"+item.Original.ArtifactID.String())){return v2input.ErrInvalid}
 seen:=map[uuid.UUID]bool{}
 for _,report:=range item.Reports{if !validImportRef(report)||report.ArtifactID==item.Original.ArtifactID||seen[report.ArtifactID]{return v2input.ErrInvalid};seen[report.ArtifactID]=true}
 return nil
}
