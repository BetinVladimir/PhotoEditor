package interchange
import(
 "context"
 "path"
 "strings"
 "unicode/utf8"
 "github.com/google/uuid"
 "golang.org/x/text/cases"
 "golang.org/x/text/unicode/norm"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/workflow"
)
// ImportedImageEvidence must be loaded from adopted ParsedInterchange plus
// current immutable artifact evidence. Archive labels alone are not authority.
type ImportedImageEvidence struct{
 EntryID uuid.UUID
 ExternalID string
 Path string
 OriginalSHA string
 Content v2input.Ref
 Bytes int64
}
type UnsupportedIngestEntry struct{Ordinal int;EntryID uuid.UUID;Reason string}
type ImportedIngestPlan struct{
 Spec workflow.IngestSpec
 Graph workflow.IngestPlan
 Unsupported []UnsupportedIngestEntry
}
// CompileImportedIngest reuses the existing verify/inspect/decode compiler with
// unchanged RAW/ICC/decode options. Missing profiles are not guessed. Unsupported
// image formats retain their original mapping ordinal in an explicit report.
func CompileImportedIngest(job uuid.UUID,images []ImportedImageEvidence)(ImportedIngestPlan,error){
 invalid:=func()(ImportedIngestPlan,error){return ImportedIngestPlan{},domain.NewError("VALIDATION_FAILED",422)}
 if job==uuid.Nil||len(images)<1||len(images)>5000{return invalid()}
 result:=ImportedIngestPlan{Spec:workflow.IngestSpec{RunID:uuid.NewSHA1(job,[]byte("interchange:canonical-ingest:1")),Items:[]workflow.IngestItem{}},Unsupported:[]UnsupportedIngestEntry{}}
 suffixes:=map[string]bool{}
 for _,suffix:=range strings.Fields(".jpg .jpeg .png .tif .tiff .nef .nrw .crw .cr2 .cr3 .arw .srf .sr2 .mrw .dng .rw2 .orf .raf .pef .srw .3fr .fff .iiq .mos .kdc .mef .x3f"){suffixes[suffix]=true}
 ids:=map[uuid.UUID]bool{};externals:=map[string]bool{};paths:=map[string]bool{};contentIDs:=map[uuid.UUID]bool{}
 for ordinal,image:=range images{
  if image.EntryID==uuid.Nil||ids[image.EntryID]||image.ExternalID==""||len(image.ExternalID)>240||!utf8.ValidString(image.ExternalID)||externals[image.ExternalID]{return invalid()}
  if image.Path==""||len(image.Path)>240||!utf8.ValidString(image.Path)||!norm.NFC.IsNormalString(image.Path)||strings.HasPrefix(image.Path,"/")||strings.ContainsAny(image.Path,"\\:\x00")||paths[cases.Fold().String(image.Path)]{return invalid()}
  for _,part:=range strings.Split(image.Path,"/"){if part==""||part=="."||part==".."{return invalid()}}
  if image.Content.ArtifactID==uuid.Nil||contentIDs[image.Content.ArtifactID]||image.Content.SHA256!=image.OriginalSHA||image.Bytes<1{return invalid()}
  if _,err:=domain.ParseDigest(image.OriginalSHA);err!=nil{return invalid()}
  ids[image.EntryID]=true;externals[image.ExternalID]=true;paths[cases.Fold().String(image.Path)]=true;contentIDs[image.Content.ArtifactID]=true
  suffix:=strings.ToLower(path.Ext(image.Path))
  reason:="";if !suffixes[suffix]{reason="unsupported_image_format"}else if image.Bytes>536870912{reason="ingest_file_limit"}
  if reason!=""{result.Unsupported=append(result.Unsupported,UnsupportedIngestEntry{ordinal,image.EntryID,reason});continue}
  result.Spec.Items=append(result.Spec.Items,workflow.IngestItem{Ordinal:int64(ordinal),Source:review.ArtifactRef{ID:image.Content.ArtifactID,SHA256:image.Content.SHA256},Bytes:image.Bytes,Suffix:suffix,Analyze:false,Tile:512})
 }
 if len(result.Spec.Items)==0{return result,nil}
 registry,err:=workflow.LoadRegistry();if err!=nil{return ImportedIngestPlan{},err}
 result.Graph,err=workflow.CompileIngest(result.Spec,registry);if err!=nil{return ImportedIngestPlan{},err}
 return result,nil
}
// PrepareInspection requires the caller to persist/authorize the dedicated
// ingest run and its source snapshot first; it never forges a WorkingImage.
func(p ImportedIngestPlan)PrepareInspection(ctx context.Context,tenant uuid.UUID,coordinator workflow.IngestInspectionCoordinator)error{
 if err:=ctx.Err();err!=nil{return err}
 if tenant==uuid.Nil||len(p.Spec.Items)==0{return domain.NewError("VALIDATION_FAILED",422)}
 return coordinator.PrepareInspection(ctx,tenant,p.Spec)
}
