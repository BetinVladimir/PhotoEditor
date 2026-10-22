package workflow

import (
 "context"
 "encoding/json"
 "errors"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

type editorCoverageStep struct {
 TaskID uuid.UUID `json:"task_id"`
 ResultID uuid.UUID `json:"result_id"`
 Attempt int `json:"attempt"`
 Fence int64 `json:"fence"`
 Operation string `json:"operation"`
 Source review.ArtifactRef `json:"source"`
 Output review.ArtifactRef `json:"output"`
 Report *review.ArtifactRef `json:"report"`
}
type editorSealOrigin struct {
 InheritedReview bool `json:"inherited_review"`
 Run uuid.UUID `json:"run_id"`
 AssetVersion uuid.UUID `json:"asset_version_id"`
 SourceID uuid.UUID `json:"source_artifact_id"`
 SourceSHA string `json:"source_sha"`
 SourceCanvas string `json:"source_canvas_hash"`
 RecipeID uuid.UUID `json:"frozen_recipe_artifact_id"`
 RecipeSHA string `json:"frozen_recipe_sha"`
 StyleID uuid.UUID `json:"executed_style_artifact_id"`
 StyleSHA string `json:"executed_style_sha"`
 CompilationSHA string `json:"compilation_sha"`
 ProcessedID uuid.UUID `json:"processed_source_artifact_id"`
 ProcessedSHA string `json:"processed_source_sha"`
 QualityID uuid.UUID `json:"quality_artifact_id"`
 QualitySHA string `json:"quality_sha"`
 EffectID *uuid.UUID `json:"effect_source_artifact_id"`
 EffectSHA *string `json:"effect_source_sha"`
 EffectResultID *uuid.UUID `json:"effect_source_result_artifact_id"`
 EffectResultSHA *string `json:"effect_source_result_sha"`
 EditorSource struct{SourceResult *review.ArtifactRef `json:"source_result"`} `json:"editor_source"`
}
type editorSealDocument struct {
 Params json.RawMessage
 SHA string
 Inputs []review.ArtifactRef
 Profile json.RawMessage
 CanvasHash string
 TraceSHA string
 Generated *review.MaskRef
 Generative,InheritedReview bool
}

// This compiler reads the canonical SQL-scoped closure through bounded hashed
// IO. It prepares a standalone CPU seal, never a render.compose shortcut or a
// replacement for the independently replayed adoption gate.
func buildEditorSealDocument(ctx context.Context,objects ArtifactOpener,bucket string,originRaw,traceRaw,inventoryRaw []byte)(editorSealDocument,error){
 invalid:=func()(editorSealDocument,error){return editorSealDocument{},errors.New("editor seal provenance invalid")}
 var origin editorSealOrigin;var trace []editorCoverageStep;var inventory []styleArtifact
 if json.Unmarshal(originRaw,&origin)!=nil||json.Unmarshal(traceRaw,&trace)!=nil||json.Unmarshal(inventoryRaw,&inventory)!=nil||origin.Run==uuid.Nil||origin.AssetVersion==uuid.Nil||len(trace)>128||len(inventory)==0||len(inventory)>4096{return invalid()}
 index:=map[uuid.UUID]styleArtifact{};inputs:=make([]review.ArtifactRef,0,len(inventory))
 for _,artifact:=range inventory{if artifact.ArtifactID==uuid.Nil||artifact.Bucket!=bucket||artifact.Bytes<1||domainDigestInvalid(artifact.SHA256)||artifact.VersionID==""||artifact.VersionID=="null"{return invalid()};if _,seen:=index[artifact.ArtifactID];seen{return invalid()};index[artifact.ArtifactID]=artifact;inputs=append(inputs,artifact.ref())}
 read:=func(ref review.ArtifactRef)([]byte,error){a,ok:=index[ref.ID];if !ok||a.ref()!=ref{return nil,errors.New("editor seal undeclared document")};return readApplyJSON(ctx,objects,bucket,a)}
 working:=func(ref review.ArtifactRef)(review.WorkingImage,error){raw,err:=read(ref);if err!=nil{return review.WorkingImage{},err};return review.ParseWorkingImage(raw)}
 original:=review.ArtifactRef{ID:origin.SourceID,SHA256:origin.SourceSHA};processed:=review.ArtifactRef{ID:origin.ProcessedID,SHA256:origin.ProcessedSHA}
 recipe:=review.ArtifactRef{ID:origin.RecipeID,SHA256:origin.RecipeSHA};style:=review.ArtifactRef{ID:origin.StyleID,SHA256:origin.StyleSHA};quality:=review.ArtifactRef{ID:origin.QualityID,SHA256:origin.QualitySHA}
 for _,ref:=range []review.ArtifactRef{original,processed,recipe,style,quality}{if ref.ID==uuid.Nil||domainDigestInvalid(ref.SHA256){return invalid()}}
 source,err:=working(original);if err!=nil{return editorSealDocument{},err};sourceCanvas,err:=canonical.Hash(source.Canvas);if err!=nil||sourceCanvas!=origin.SourceCanvas{return invalid()}
 final,err:=working(processed);if err!=nil{return editorSealDocument{},err};canvas,err:=canonical.Hash(final.Canvas);if err!=nil||final.Color!=source.Color{return invalid()}
 initial:=original;parent:=origin.EditorSource.SourceResult
 if origin.EffectID!=nil{if origin.EffectSHA==nil||origin.EffectResultID==nil||origin.EffectResultSHA==nil{return invalid()};initial=review.ArtifactRef{ID:*origin.EffectID,SHA256:*origin.EffectSHA};parent=&review.ArtifactRef{ID:*origin.EffectResultID,SHA256:*origin.EffectResultSHA}}
 initialImage,err:=working(initial);if err!=nil{return editorSealDocument{},err}
 inheritedReview:=origin.InheritedReview
 var generated *review.MaskRef
 if parent!=nil{raw,e:=read(*parent);if e!=nil{return editorSealDocument{},e};document,e:=v2draft.ParseImageResult(raw,v2input.Ref{ArtifactID:parent.ID,SHA256:parent.SHA256});if e!=nil||document.WorkingImage!=(v2input.Ref{ArtifactID:initial.ID,SHA256:initial.SHA256}){return invalid()};reportRaw,e:=read(review.ArtifactRef{ID:document.Report.ArtifactID,SHA256:document.Report.SHA256});if e!=nil{return editorSealDocument{},e};var parentReport struct{Status string `json:"status"`};if json.Unmarshal(reportRaw,&parentReport)!=nil||(parentReport.Status!="pass"&&parentReport.Status!="review"){return invalid()};inheritedReview=inheritedReview||parentReport.Status=="review";if document.GeneratedMask!=nil{raw,e=read(review.ArtifactRef{ID:document.GeneratedMask.ArtifactID,SHA256:document.GeneratedMask.SHA256});if e!=nil{return editorSealDocument{},e};mask,e:=review.ParseMaskRef(raw);if e!=nil||mask.Protected||mask.Selector!="generated"||mask.Canvas!=initialImage.Canvas{return invalid()};generated=&mask}}
 maxPixels:=initialImage.Canvas.Width*initialImage.Canvas.Height;previous:=initial;seen:=map[uuid.UUID]bool{};generative:=generated!=nil
 for _,step:=range trace{if step.TaskID==uuid.Nil||step.ResultID==uuid.Nil||step.Attempt<1||step.Fence<1||seen[step.TaskID]||step.Source!=previous{return invalid()};seen[step.TaskID]=true;image,e:=working(step.Output);if e!=nil{return editorSealDocument{},e};if image.Color!=source.Color{return invalid()};pixels:=image.Canvas.Width*image.Canvas.Height;if pixels>maxPixels{maxPixels=pixels};if step.Operation=="image.generate"||step.Operation=="image.inpaint"||step.Operation=="image.outpaint"{generative=true;if step.Report==nil{return invalid()};if _,e=read(*step.Report);e!=nil{return editorSealDocument{},e}}else if step.Report!=nil{return invalid()};previous=step.Output}
 if previous!=processed{return invalid()}
 qualityRaw,err:=read(quality);if err!=nil{return editorSealDocument{},err};var report struct{Target review.ArtifactRef `json:"target"`;Status string `json:"status"`;Technical struct{Finite bool `json:"finite"`} `json:"technical"`};if json.Unmarshal(qualityRaw,&report)!=nil||report.Target!=processed||!report.Technical.Finite||(report.Status!="pass"&&report.Status!="review"){return invalid()}
 inheritedReview=inheritedReview||report.Status=="review"
 traceSHA,err:=canonical.Hash(trace);if err!=nil{return invalid()};params:=map[string]any{"schema_version":1,"build_run_id":origin.Run,"original_recipe":recipe,"original_source":v2input.SessionVersionBinding{AssetVersionID:origin.AssetVersion,Source:v2input.Ref{ArtifactID:original.ID,SHA256:original.SHA256},CanvasHash:sourceCanvas},"processed_source":v2input.SessionVersionBinding{AssetVersionID:origin.AssetVersion,Source:v2input.Ref{ArtifactID:processed.ID,SHA256:processed.SHA256},CanvasHash:canvas},"quality":quality,"executed_style":style,"compilation_sha":origin.CompilationSHA,"trace_sha":traceSHA,"generative_requested":generative,"generated":generated,"inherited_review":inheritedReview};if len(trace)>0{params["coverage_trace"]=trace}
 raw,err:=canonical.Marshal(params);if err!=nil||len(raw)>1048576{return invalid()};sha,err:=canonical.Hash(json.RawMessage(raw));if err!=nil{return invalid()}
 finalPixels:=final.Canvas.Width*final.Canvas.Height
 profile,err:=canonical.Marshal(map[string]any{"backend":"cpu","precision":"float32","tile_size":512,"resource_estimate":ResourceEstimate{RAMBytes:32*maxPixels+(64<<20),ScratchBytes:48*maxPixels+(64<<20),OutputBytes:8*finalPixels+(6<<20),PixelCount:finalPixels}});if err!=nil{return invalid()}
 return editorSealDocument{Params:raw,SHA:sha,Inputs:inputs,Profile:profile,CanvasHash:canvas,TraceSHA:traceSHA,Generated:generated,Generative:generative,InheritedReview:inheritedReview},nil
}
