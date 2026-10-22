package v2render

import (
 "bytes"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"

 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// AuthoredFinalSchemasSHA is the exact root-reviewed additive compose/2 bundle.
// Reader rollout accepts historical generations separately; new compose/2
// builders cannot emit an old generation which lacks the Params contract.
const AuthoredFinalSchemasSHA="55c5e551578614890def6135458f535fa71d1d1725a6525d490588b3f29a4772"

type AuthoredFinalExecutionProfile struct {
 Backend string `json:"backend"`
 Renderer string `json:"renderer_version"`
 KernelVersion string `json:"authored_kernel_version"`
 Operation string `json:"operation"`
 Version string `json:"operation_version"`
 ContractsSHA string `json:"contracts_sha"`
 RuntimeSHA string `json:"render_runtime_sha"`
 ModelSHA string `json:"model_lock_sha"`
 ICCSHA string `json:"icc_lock_sha"`
 TileSize int `json:"tile_size"`
 AlphaMode string `json:"alpha_mode"`
 Resources LayerResourceEstimate `json:"resource_estimate"`
 Provider RenderProviderPeak `json:"retained_stage_peak"`
 Qualification RenderQualificationPin `json:"native_qualification_pin"`
 RequiredReview bool `json:"review_required"`
}

// CompileAuthoredFinalExecutionProfile binds the original full recipe and the
// actual final target, then selects a distinct compose/2 calibration at TARGET
// dimensions. Upstream GPU work has its own profiles and is never relabelled
// as GPU work of this host comparison/publication kernel. This current concrete
// kernel is CPU; unsupported backends fail rather than acquire a false label.
// Calibration/qualification IO and current SQL authority remain external gates.
func CompileAuthoredFinalExecutionProfile(request RenderDraftRequest,params CompiledAuthoredFinalParams,targetRaw []byte,qualification RenderQualification,calibration RenderCalibration)(json.RawMessage,error){
 fail:=func()(json.RawMessage,error){return nil,v2input.ErrInvalid}
 verified,err:=CompileAuthoredFinalParams(params.Params);if err!=nil||!bytes.Equal(verified.JSON,params.JSON){return fail()}
 if qualification.Backend!="cpu"||params.Params.TenantID!=request.Tenant||params.Params.Recipe!=request.RecipeRef{return fail()}
 evidence,err:=CompileRenderEvidence(request.Binding,request.RecipeRef,request.RecipeRaw,request.SourceRaw);if err!=nil{return fail()}
 if qualification.ModelSHA!=evidence.Recipe.Scalars.ModelLockSHA{return fail()}
 if err=calibration.validate(qualification);err!=nil{return fail()}
 if len(targetRaw)<2||len(targetRaw)>1048576{return fail()}
 sum:=sha256.Sum256(targetRaw);wire,err:=canonical.Bytes(targetRaw)
 if err!=nil||!bytes.Equal(wire,targetRaw)||hex.EncodeToString(sum[:])!=params.Params.Source.Source.SHA256{return fail()}
 target,err:=review.ParseWorkingImage(targetRaw);if err!=nil{return fail()}
 canvasSHA,err:=canonical.Hash(target.Canvas)
 if err!=nil||canvasSHA!=params.Params.Source.CanvasHash||params.Params.Source.AssetVersionID!=request.Binding.AssetVersionID||target.Color!=evidence.Source.Color||target.Canvas.SourceSHA256!=evidence.Source.Canvas.SourceSHA256{return fail()}
 selected,err:=calibration.Select(qualification,int64(target.Canvas.Width),int64(target.Canvas.Height),evidence.Recipe.Scalars.ICCLockSHA);if err!=nil{return fail()}
 if selected.Provider.VRAMBytes!=0{return fail()}
 // Matches prepare_geometry_final: retained arrays plus real change-map
 // scratch. No sampled memory observation is presented as a measured peak.
 pixels:=int64(target.Canvas.Width)*int64(target.Canvas.Height)
 peak:=selected.Provider;peak.RAMBytes+=40*pixels;peak.ScratchBytes+=4*pixels+8192
 resources,err:=RenderResources(int64(target.Canvas.Width),int64(target.Canvas.Height),1,1,calibration.TileSize,calibration.AlphaMode,peak);if err!=nil{return fail()}
 calibrationSHA,err:=canonical.Hash(calibration);if err!=nil{return fail()}
 profile:=AuthoredFinalExecutionProfile{Backend:"cpu",Renderer:"v2.2",KernelVersion:"v2.authored.1",Operation:"render.compose",Version:"2",ContractsSHA:AuthoredFinalSchemasSHA,
  RuntimeSHA:qualification.RuntimeSHA,ModelSHA:qualification.ModelSHA,ICCSHA:evidence.Recipe.Scalars.ICCLockSHA,
  TileSize:calibration.TileSize,AlphaMode:calibration.AlphaMode,Resources:resources,Provider:selected.Provider,
  Qualification:RenderQualificationPin{RenderQualification:qualification,CalibrationSHA:calibrationSHA},RequiredReview:true}
 return canonical.Marshal(profile)
}
