package interchange

import (
 "context"
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type importProposalReportAuthority struct{archived,prepared int;denyArchive,denyPrepared bool}
func(a *importProposalReportAuthority)ValidateArchivedImportDefectProposeReport(context.Context,[]byte,v2input.SessionVersionBinding)error{a.archived++;if a.denyArchive{return v2input.ErrInvalid};return nil}
func(a *importProposalReportAuthority)QualifyImportedDefectProposeReport(context.Context,[]byte,v2input.SessionVersionBinding)error{a.prepared++;if a.denyPrepared{return v2input.ErrInvalid};return nil}

// Mock authorities verify codec order and preservation, not physical provenance.
func TestImportDefectProposeReportRemapsCompleteNoFaceClosure(t *testing.T){
 _,_,imageRaw:=importWorkingValidatorFixture(t)
 image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
 canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
 ref:=func(s string)v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(s))}}
 old:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:ref("image"),CanvasHash:canvas}
 target:=old;target.AssetVersionID=uuid.New();target.Source.ArtifactID=uuid.New()
 mask:=map[string]any{"artifact":ref("mask"),"canvas":image.Canvas,"transform_chain":json.RawMessage(image.TransformChain),"object_id":nil,"model_lock_id":nil,"selector":"person","confidence":1,"protected":true}
 report:=map[string]any{"params":map[string]any{"image":old.Source,"faces":[]any{},"permanent":nil,"diameter_face_fraction":[]float64{.002,.012},"protections":[]any{}},"proposal":ref("proposal"),"protection":mask,"label_png":ref("png"),"faces":[]any{},"warnings":[]string{"no_faces"},"algorithm":"defect_dog_v1"}
 raw,err:=canonical.Marshal(report);if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"valid","archive_denied","target_denied","mask_denied","missing_png","unchanged","cancelled"}{t.Run(mode,func(t *testing.T){
  authority:=&importProposalReportAuthority{denyArchive:mode=="archive_denied",denyPrepared:mode=="target_denied"}
  maskAuthority:=&importMaskQualifierStub{};if mode=="mask_denied"{maskAuthority.failure=v2input.ErrInvalid}
  codec:=ImportDefectProposeReportCodec{Qualifier:authority,Masks:ImportMaskCodec{Qualifier:maskAuthority}}
  var contracts []string
  resolve:=func(original v2input.Ref,kind string)(v2input.Ref,error){
   contracts=append(contracts,kind)
   if mode=="missing_png"&&kind=="DefectLabelPNG"{return v2input.Ref{},v2input.ErrInvalid}
   if mode=="unchanged"{return original,nil}
   return v2input.Ref{ArtifactID:uuid.New(),SHA256:original.SHA256},nil
  }
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  prepared,err:=codec.RemapImportDocument(ctx,"DefectProposeReport",raw,old,target,resolve)
  if mode!="valid"{if err==nil||prepared!=nil{t.Fatal("unqualified report accepted")};if (mode=="archive_denied"||mode=="cancelled")&&len(contracts)!=0{t.Fatal("resolved before source authorization")};return}
  if err!=nil||authority.archived!=1||authority.prepared!=1||maskAuthority.calls!=2{t.Fatal("full qualification missing",err)}
  if len(contracts)!=3||contracts[0]!="DefectCandidates"||contracts[1]!="DefectLabelPNG"||contracts[2]!="MaskPixels"{t.Fatal(contracts)}
  if err:=validateImportDefectProposeReport(prepared,target.Source);err!=nil{t.Fatal("target structure",err)}
  var result map[string]json.RawMessage;if json.Unmarshal(prepared,&result)!=nil{t.Fatal("prepared report")}
  if result["label_png"]==nil||result["protection"]==nil||result["warnings"]==nil{t.Fatal("report evidence dropped")}
 })}
}
