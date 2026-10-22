package v2render

import (
 "bytes"
 "context"
 "encoding/json"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)
type LensApplyDecision struct {
 SchemaVersion int `json:"schema_version"`
 ResultID uuid.UUID `json:"result_id"`
 Root v2input.Ref `json:"root"`
 WorkingImage v2input.Ref `json:"working_image"`
 ConfirmGeometryReset bool `json:"confirm_geometry_reset"`
 AcceptUncoveredArea bool `json:"accept_uncovered_area"`
}
func DecodeLensApplyDecision(raw []byte)(LensApplyDecision,error){
 var value LensApplyDecision
 if len(raw)<2 || len(raw)>65536{return value,v2input.ErrInvalid}
 normalized,err:=canonical.Bytes(raw);if err!=nil || json.Unmarshal(normalized,&value)!=nil{return LensApplyDecision{},v2input.ErrInvalid}
 exact,err:=canonical.Marshal(value)
 if err!=nil || !bytes.Equal(exact,normalized) || value.SchemaVersion!=1 || value.ResultID==uuid.Nil || value.Root.ArtifactID==uuid.Nil || value.WorkingImage.ArtifactID==uuid.Nil || !validSHA256(value.Root.SHA256) || !validSHA256(value.WorkingImage.SHA256) || !value.ConfirmGeometryReset{return LensApplyDecision{},v2input.ErrInvalid}
 return value,nil
}
func(s LensProposalService)Apply(ctx context.Context,who auth.Identity,tenantID,project,version,producer uuid.UUID,decision LensApplyDecision)(PerspectiveApplied,error){
 zero:=PerspectiveApplied{}
 if err:=ctx.Err();err!=nil{return zero,err}
 raw,err:=canonical.Marshal(decision);if err!=nil{return zero,err};if _,err=DecodeLensApplyDecision(raw);err!=nil{return zero,err}
 current,err:=s.Describe(ctx,who,tenantID,project,version,producer);if err!=nil{return zero,err}
 if decision.ResultID!=current.ResultID || decision.Root!=current.Result || decision.WorkingImage!=current.WorkingImage || (current.RequiresReview && !decision.AcceptUncoveredArea){return zero,v2input.ErrExpired}
 catalog,_,err:=BuildAdoptedLensCatalog(ctx,SQLAdoptedLensSource{DB:s.DB},s.Objects,who,tenantID,project,version,producer,current.Result,s.Bucket);if err!=nil{return zero,err}
 image:=catalog.documents[current.WorkingImage.ArtifactID]
 // Bypass is a durable no-op and must still verify original bytes.
 if current.WorkingImage==current.Base.Source {
  // Describe already read and verified the original; repeat through current scope.
  proposal,e:=s.Describe(ctx,who,tenantID,project,version,producer);if e!=nil{return zero,e};if proposal!=current{return zero,v2input.ErrExpired}
  return PerspectiveApplied{Version:version,Parent:version,Source:current.WorkingImage},nil
 }
 if len(image)<2{return zero,v2input.ErrInvalid}
 bounded,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 var saved *uuid.UUID
 err=tenant.WithTenant(bounded,s.DB,who,tenantID,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  if _,e:=tx.Exec(bounded,"SET LOCAL statement_timeout='15s'");e!=nil{return e}
  if e:=tx.QueryRow(bounded,`SELECT photo.apply_v25_lens_version($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10)`,tenantID,project,version,producer,who.UserID,who.SessionID,string(raw),string(catalog.documents[current.Result.ArtifactID]),string(image),string(catalog.documents[current.Report.ArtifactID])).Scan(&saved);e!=nil{return e}
  expected:=uuid.NewSHA1(current.ResultID,[]byte("V25.LensAppliedVersion"));if saved==nil || *saved!=expected{return v2input.ErrExpired};return bounded.Err()
 })
 if e:=bounded.Err();e!=nil{return zero,e};if err!=nil{return zero,err}
 return PerspectiveApplied{Version:*saved,Parent:version,Source:current.WorkingImage},nil
}
