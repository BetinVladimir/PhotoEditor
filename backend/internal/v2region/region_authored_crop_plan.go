package v2region

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "sort"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2render"
)

type AuthoredCropParams struct {
 SchemaVersion int `json:"schema_version"`
 IntentSHA string `json:"intent_sha256"`
 Tenant uuid.UUID `json:"tenant_id"`
 Project uuid.UUID `json:"project_id"`
 Request uuid.UUID `json:"request_id"`
 Asset uuid.UUID `json:"asset_id"`
 RenderRun uuid.UUID `json:"render_run_id"`
 DraftDocument uuid.UUID `json:"draft_document_id"`
 DraftRevision int64 `json:"draft_revision"`
 FrozenDocument uuid.UUID `json:"frozen_document_id"`
 Source v2input.SessionVersionBinding `json:"source"`
 FrozenRecipe v2input.Ref `json:"frozen_recipe"`
 RenderResult v2input.Ref `json:"render_result"`
 RenderedImage v2input.Ref `json:"rendered_image"`
 RenderReport v2input.Ref `json:"render_report"`
 Rect RegionRect `json:"rect"`
 MaxEdge int `json:"max_edge"`
 TargetICCSHA string `json:"target_icc_sha256"`
}

// AuthoredCropPlan binds physical immutable document bytes. SQL must separately
// prove the selected render task/result/attempt/fence/adoption and live current
// draft at publication, sealing, claim, heartbeat and commit. This compiler
// cannot turn an arbitrary same-tenant WorkingImage into that authority.
type AuthoredCropPlan struct {
 intent AuthoredRegionIntent
 params AuthoredCropParams
 body []byte
 sha string
 required []v2input.Entry
 source,rendered review.WorkingImage
 reviewRequired bool
}
func(p AuthoredCropPlan)JSON()[]byte{return append([]byte(nil),p.body...)}
func(p AuthoredCropPlan)SHA256()string{return p.sha}

func CompileAuthoredCropPlan(record AuthoredRegionRecord,renderPart uuid.UUID,resultRef v2input.Ref,
 recipeRaw,sourceRaw,resultRaw,imageRaw,reportRaw []byte)(AuthoredCropPlan,error){
 invalid:=func()(AuthoredCropPlan,error){return AuthoredCropPlan{},ErrRegionInvalid}
 if err:=record.validate();err!=nil{return invalid()}
 i:=record.Intent;hash,err:=i.SHA256()
 if err!=nil||hash!=record.SHA||record.ID==uuid.Nil||record.CancelRequested||!record.Current||renderPart!=uuid.NewSHA1(record.RenderBatch,[]byte("render.batch:0")){return invalid()}
 evidence,err:=v2render.CompileRenderEvidence(i.Binding,i.Recipe,recipeRaw,sourceRaw);if err!=nil{return invalid()}
 root,err:=v2draft.ParseImageResult(resultRaw,resultRef);if err!=nil{return invalid()}
 for _,item:=range []struct{ref v2input.Ref;raw []byte}{{resultRef,resultRaw},{root.WorkingImage,imageRaw},{root.Report,reportRaw}}{
  if item.ref.ArtifactID==uuid.Nil||len(item.raw)<2||len(item.raw)>1048576{return invalid()}
  sum:=sha256.Sum256(item.raw);normalized,e:=canonical.Bytes(item.raw)
  if e!=nil||!bytes.Equal(normalized,item.raw)||hex.EncodeToString(sum[:])!=item.ref.SHA256{return invalid()}
 }
 image,err:=review.ParseWorkingImage(imageRaw);if err!=nil||image.DType!="float32"||image.Color!=evidence.Source.Color||image.Canvas.SourceSHA256!=evidence.Source.Canvas.SourceSHA256{return invalid()}
 report,err:=v2draft.ParseImageReport(reportRaw,root.Report);if err!=nil||report.Operation!="render.compose"||(report.Status!="pass"&&report.Status!="review"){return invalid()}
 sourcePresent:=false;for _,source:=range report.Sources{if source==i.Binding{sourcePresent=true}}
 if !sourcePresent{return invalid()}
 var before,after struct{Steps []json.RawMessage `json:"steps"`}
 if json.Unmarshal(evidence.Source.TransformChain,&before)!=nil||json.Unmarshal(image.TransformChain,&after)!=nil||len(after.Steps)<len(before.Steps){return invalid()}
 for n,step:=range before.Steps{a,e:=canonical.Bytes(step);b,f:=canonical.Bytes(after.Steps[n]);if e!=nil||f!=nil||!bytes.Equal(a,b){return invalid()}}
 params:=AuthoredCropParams{1,record.SHA,i.Tenant,i.Project,record.ID,i.Asset,renderPart,i.DraftDocument,i.DraftRevision,i.FrozenDocument,i.Binding,i.Recipe,resultRef,root.WorkingImage,root.Report,i.Rect,i.MaxEdge,i.TargetICCSHA}
 body,err:=canonical.Marshal(params);if err!=nil{return invalid()};digest,err:=canonical.Hash(params);if err!=nil{return invalid()}
 required:=[]v2input.Entry{{Ref:i.Binding.Source,Contract:"WorkingImage"},{Ref:i.Recipe,Contract:"FrozenRenderRecipe"},{Ref:resultRef,Contract:"ImageResult"},{Ref:root.WorkingImage,Contract:"WorkingImage"},{Ref:root.Report,Contract:"ImageReport"},
  {Ref:v2input.Ref{ArtifactID:image.Pixels.ID,SHA256:image.Pixels.SHA256},Contract:"NpyArray"}}
 if image.Alpha!=nil{required=append(required,v2input.Entry{Ref:v2input.Ref{ArtifactID:image.Alpha.ID,SHA256:image.Alpha.SHA256},Contract:"NpyArray"})}
 var chain struct{Steps []struct{Inverse *v2input.Ref `json:"inverse_field"`} `json:"steps"`}
 if json.Unmarshal(image.TransformChain,&chain)!=nil{return invalid()}
 for _,step:=range chain.Steps{if step.Inverse!=nil{required=append(required,v2input.Entry{Ref:*step.Inverse,Contract:"NpyArray"})}}
 return AuthoredCropPlan{intent:i,params:params,body:body,sha:digest,required:required,source:evidence.Source,rendered:image,reviewRequired:report.Status=="review"},nil
}

// FreezeInputs consumes the complete scoped resolver inventory, not a scan of
// arbitrary JSON. Required coverage is checked here; the SQL resolver owns
// authorization and full typed descendant closure, including ImageResult masks
// and the full authored recipe's style/layer dependencies.
func(p AuthoredCropPlan)FreezeInputs(task uuid.UUID,params v2input.Ref,resolved []v2input.Entry)(FrozenRegionInputs,error){
 if task==uuid.Nil||params.ArtifactID==uuid.Nil||params.SHA256!=p.sha||len(p.body)==0||len(resolved)<4||len(resolved)>4095{return FrozenRegionInputs{},ErrRegionInvalid}
 entries:=append([]v2input.Entry(nil),resolved...);entries=append(entries,v2input.Entry{Ref:params,Contract:"Params"})
 sort.Slice(entries,func(i,j int)bool{return bytes.Compare(entries[i].Ref.ArtifactID[:],entries[j].Ref.ArtifactID[:])<0})
 lookup:=map[uuid.UUID]v2input.Entry{}
 for _,entry:=range entries{if entry.Ref.ArtifactID==uuid.Nil||!validRegionSHA(entry.Ref.SHA256)||len(entry.Contract)<1||len(entry.Contract)>120{return FrozenRegionInputs{},ErrRegionInvalid};if _,ok:=lookup[entry.Ref.ArtifactID];ok{return FrozenRegionInputs{},ErrRegionInvalid};lookup[entry.Ref.ArtifactID]=entry}
 for _,entry:=range p.required{if lookup[entry.Ref.ArtifactID]!=entry{return FrozenRegionInputs{},ErrRegionInvalid}}
 digest,err:=canonical.Hash(struct{Task uuid.UUID `json:"task"`;Operation string `json:"operation"`;Version string `json:"version"`;PlanSHA string `json:"plan_sha256"`;Entries []v2input.Entry `json:"entries"`}{task,"region.authored_crop","1",p.sha,entries});if err!=nil{return FrozenRegionInputs{},err}
 return FrozenRegionInputs{Open:func(ctx context.Context)(v2input.Cursor,error){if err:=ctx.Err();err!=nil{return nil,err};return &regionInputCursor{entries:entries},nil},Direct:[]v2input.Ref{params},InputDigest:digest},nil
}
