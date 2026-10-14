package interchange

import (
 "bytes"
 "context"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// These bytes must be loaded from exact immutable origin objects. This check
// proves content bindings only, not current model qualification or SQL scope.
type ExportStatsPersonDocuments struct {
 Root []byte
 Params []byte
 Report []byte
}
func VerifyExportStatsPersonDocuments(ctx context.Context,origin ExportStatsPersonOrigin,documents ExportStatsPersonDocuments,statsParamsRaw,imageRaw []byte,binding v2input.SessionVersionBinding,modelLock,backend string)error{
 if err:=ctx.Err();err!=nil{return err}
 for _,item:=range []struct{ref v2input.Ref;body []byte;maximum int}{
  {origin.Root,documents.Root,1<<20},{origin.Params,documents.Params,65536},{origin.Report,documents.Report,1<<20},
 }{
  if !validImportRef(item.ref)||len(item.body)<2||len(item.body)>item.maximum||rawSHA(item.body)!=item.ref.SHA256{return v2input.ErrInvalid}
 }
 stats,err:=review.ParseStatsParams(statsParamsRaw);if err!=nil{return err}
 image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{return err}
 if rawSHA(imageRaw)!=binding.Source.SHA256||stats.Image.ID!=binding.Source.ArtifactID||stats.Image.SHA256!=binding.Source.SHA256{return v2input.ErrInvalid}
 if err:=stats.ValidateSource(image,stats.Image);err!=nil{return err}
 // Exact canonical equality includes confidence, protected flag, object/model
 // identity and the full transform chain, rather than just the pixels ref.
 inline,err:=canonical.Marshal(stats.Person);if err!=nil{return err}
 if !bytes.Equal(inline,documents.Root)||origin.Mask.SHA256!=stats.Person.Artifact.SHA256{return v2input.ErrInvalid}
 if err:=v2input.VerifySemanticPersonEvidence(binding,imageRaw,origin.Root,documents.Root,documents.Params,documents.Report,modelLock,backend);err!=nil{return err}
 return ctx.Err()
}
