package interchange

import (
 "context"
 "encoding/json"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

// Policies match the installed renderer's closed color/raw ABI. The native
// StyleV2 probe independently validates complete operators, curves and models.
type BoundImportStyleScope struct{
 WorkICCSHA string
 Masters ImportMasterReader
 Targets ImportBackgroundTargetAuthority
}
func(s BoundImportStyleScope)QualifyArchivedStyleScope(ctx context.Context,raw []byte)error{
 if err:=ctx.Err();err!=nil{return err}
 if !validImportRefForSHA(s.WorkICCSHA)||s.Masters==nil||s.Targets==nil||len(raw)<2||len(raw)>8<<20{return v2input.ErrInvalid}
 if _,err:=v2draft.DecodeStyleBaseline(raw);err!=nil{return err}
 var fields struct{Schema string `json:"schema_version"`;Renderer string `json:"renderer_version"`;Raw string `json:"raw_recipe"`;Color struct{Profile string `json:"profile_id"`} `json:"color"`}
 if err:=json.Unmarshal(raw,&fields);err!=nil{return err}
 if fields.Schema!="2.0.0"||fields.Renderer!="photo_renderer_v2"||fields.Raw!="raw_camera_wb_linear_srgb_ahd_v1"||fields.Color.Profile!="work_rec2020_gamma24_v1"{return v2input.ErrInvalid}
 return ctx.Err()
}
func(s BoundImportStyleScope)QualifyTargetStyleScope(ctx context.Context,raw []byte,binding v2input.SessionVersionBinding)error{
 if err:=s.QualifyArchivedStyleScope(ctx,raw);err!=nil{return err}
 if err:=s.Targets.QualifyImportBackgroundTarget(ctx,binding);err!=nil{return err}
 master,err:=s.Masters.ReadImportMaster(ctx,binding.Source,1<<20);if err!=nil{return err}
 master=append([]byte(nil),master...)
 if len(master)<2||len(master)>1<<20||rawSHA(master)!=binding.Source.SHA256{return v2input.ErrInvalid}
 var typed review.WorkingImage;if err:=decodeImportDocument(master,&typed);err!=nil{return err}
 image,err:=review.ParseWorkingImage(master);if err!=nil{return err}
 canvas,err:=canonical.Hash(image.Canvas);if err!=nil{return err}
 if canvas!=binding.CanvasHash||image.DType!="float32"||image.Color.ProfileID!="work_rec2020_gamma24_v1"||image.Color.Encoding!="gamma24"||image.Color.ICCSHA256!=s.WorkICCSHA{return v2input.ErrInvalid}
 // Foreign validation claims were reset by ImportStyleCodec. Explicit review
 // accepts the newly qualified values, never an inherited quality assertion.
 var fields struct{Quality struct{Status string `json:"status"`;Confidence string `json:"transfer_confidence"`} `json:"quality"`;Provenance struct{Pairs int `json:"pair_count"`;Sessions int `json:"session_count"`;Manifest *string `json:"manifest_sha256"`;Runtime *string `json:"runtime_lock_id"`} `json:"provenance"`}
 if err:=json.Unmarshal(raw,&fields);err!=nil{return err}
 if fields.Quality.Status!="needs_review"||fields.Quality.Confidence!="unvalidated"||fields.Provenance.Pairs!=0||fields.Provenance.Sessions!=0||fields.Provenance.Manifest!=nil||fields.Provenance.Runtime!=nil{return v2input.ErrInvalid}
 return ctx.Err()
}
var _ ImportStyleScopeAuthority=BoundImportStyleScope{}
