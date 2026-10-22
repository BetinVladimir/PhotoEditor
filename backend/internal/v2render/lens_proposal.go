package v2render

import (
 "context"
 "bytes"
 "crypto/sha256"
 "encoding/hex"
 "photoeditor/backend/internal/canonical"
 "encoding/json"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

type LensProposal struct {
 SchemaVersion int `json:"schema_version"`
 Tenant uuid.UUID `json:"tenant_id"`
 Project uuid.UUID `json:"project_id"`
 Producer uuid.UUID `json:"producer_task_id"`
 ResultID uuid.UUID `json:"result_id"`
 Result v2input.Ref `json:"result"`
 Base v2input.SessionVersionBinding `json:"base"`
 WorkingImage v2input.Ref `json:"working_image"`
 ChangedMask v2input.Ref `json:"changed_mask"`
 Report v2input.Ref `json:"report"`
 Canvas review.Canvas `json:"canvas"`
 Status string `json:"status"`
 RequiresReview bool `json:"requires_review"`
}
type LensProposalService struct { PreviewSigner LensPreviewSigner; SRGBSHA string; DB *pgxpool.Pool; Objects v2input.SessionAnalysisObjectReader; Bucket string }
func(s LensProposalService)Describe(ctx context.Context,who auth.Identity,tenantID,project,version,producer uuid.UUID)(LensProposal,error){
 zero:=LensProposal{}
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.DB==nil || s.Objects==nil || who.UserID==uuid.Nil || who.SessionID==uuid.Nil || tenantID==uuid.Nil || project==uuid.Nil || version==uuid.Nil || producer==uuid.Nil{return zero,v2input.ErrInvalid}
 var expected v2input.Ref
 err:=tenant.WithTenant(ctx,s.DB,who,tenantID,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  return tx.QueryRow(ctx,`SELECT artifact_id,sha256 FROM photo.v25_adopted_lens_binding($1,$2,$3,$4,$5,$6)`,tenantID,project,version,producer,who.UserID,who.SessionID).Scan(&expected.ArtifactID,&expected.SHA256)
 })
 if errors.Is(err,pgx.ErrNoRows){return zero,v2input.ErrExpired};if err!=nil{return zero,err}
 catalog,binding,err:=BuildAdoptedLensCatalog(ctx,SQLAdoptedLensSource{DB:s.DB},s.Objects,who,tenantID,project,version,producer,expected,s.Bucket)
 if err!=nil{return zero,err}
 result,err:=v2draft.ParseImageResult(catalog.documents[expected.ArtifactID],expected);if err!=nil{return zero,err}
 imageRaw:=catalog.documents[result.WorkingImage.ArtifactID]
 // A zero-strength qualified bypass references the exact original WorkingImage.
 if result.WorkingImage==binding.Base.Source {
  obj,e:=catalog.Lookup(ctx,who,tenantID,v2draft.LayerRef{Ref:result.WorkingImage,Contract:"WorkingImage",Path:"Original"});if e!=nil{return zero,e}
  reader,e:=s.Objects.Open(ctx,obj);if e!=nil{return zero,e}
  imageRaw,e=readAdoptedRoot(ctx,reader,obj.Bytes);closeErr:=reader.Close();if e!=nil{return zero,e};if closeErr!=nil{return zero,closeErr}
  // The original catalog pins metadata; verify its physical bytes independently.
  if e=verifyLensDocumentBytes(imageRaw,result.WorkingImage);e!=nil{return zero,e}
 }
 image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{return zero,err}
 var report struct {Status string `json:"status"`;Operation string `json:"operation"`}
 if json.Unmarshal(catalog.documents[result.Report.ArtifactID],&report)!=nil || report.Operation!="optical.lens" || (report.Status!="pass" && report.Status!="review"){return zero,v2input.ErrInvalid}
 if _,err=catalog.Lookup(ctx,who,tenantID,v2draft.LayerRef{Ref:expected,Contract:"ImageResult",Path:"Review"});err!=nil{return zero,err}
 if err=ctx.Err();err!=nil{return zero,err}
 return LensProposal{1,tenantID,project,producer,binding.ResultID,expected,binding.Base,result.WorkingImage,result.ChangedMask,result.Report,image.Canvas,report.Status,report.Status=="review"},nil
}

func verifyLensDocumentBytes(raw []byte,ref v2input.Ref)error{
 sum:=sha256.Sum256(raw);jcs,err:=canonical.Bytes(raw)
 if err!=nil || !bytes.Equal(jcs,raw) || hex.EncodeToString(sum[:])!=ref.SHA256{return v2input.ErrInvalid};return nil
}
