package v2region

import (
    "bytes"
    "encoding/json"
    "io"
    "sort"

    "github.com/google/uuid"
    "photoeditor/backend/internal/canonical"
    "photoeditor/backend/internal/review"
    "photoeditor/backend/internal/v2input"
)

// RegionExecutionDescriptor is a server-frozen canonical stage inventory, not
// adoption authority. It must never be decoded from a public Region request.
// SQL current-result gates and physical document verification remain mandatory.
type RegionExecutionDescriptor struct {
    SchemaVersion int `json:"schema_version"`
    Source v2input.Ref `json:"source"`
    SourceCanvasHash string `json:"source_canvas_hash"`
    Style v2input.Ref `json:"style"`
    CullingAuthority json.RawMessage `json:"culling_authority,omitempty"`
    Stages []RegionExecutionStage `json:"stages"`
}

type RegionExecutionStage struct {
    Operation string `json:"operation"`
    Phase string `json:"phase,omitempty"`
    ReviewAuthority json.RawMessage `json:"review_authority,omitempty"`
    OperationVersion int `json:"operation_version"`
    TaskID uuid.UUID `json:"task_id"`
    ResultID uuid.UUID `json:"result_id"`
    Attempt int `json:"attempt"`
    FencingToken int64 `json:"fencing_token"`
    Params v2input.Ref `json:"params"`
    Source v2input.Ref `json:"source"`
    Output v2input.Ref `json:"output"`
    Plan *v2input.Ref `json:"plan,omitempty"`
    PolicyParams *v2input.Ref `json:"policy_params,omitempty"`
    Report *v2input.Ref `json:"report,omitempty"`
    BackgroundAsset *review.ApplyBackgroundAsset `json:"background_asset,omitempty"`
    BackgroundParams *v2input.Ref `json:"background_params,omitempty"`
    BackgroundImage *v2input.Ref `json:"background_image,omitempty"`
    BackgroundCanvas *review.Canvas `json:"background_canvas,omitempty"`
    SourceCanvas review.Canvas `json:"source_canvas"`
    TargetCanvas review.Canvas `json:"target_canvas"`
    TargetHasAlpha bool `json:"target_has_alpha"`
    InputMaskCount int64 `json:"input_mask_count"`
    Transform json.RawMessage `json:"transform,omitempty"`
}

func decodeRegionExecution(raw []byte) (RegionExecutionDescriptor,error) {
    var out RegionExecutionDescriptor
    if len(raw)<2 || len(raw)>1048576 { return out,ErrRegionInvalid }
    canon,err:=canonical.Bytes(raw)
    if err!=nil || !bytes.Equal(canon,raw) { return out,ErrRegionInvalid }
    decoder:=json.NewDecoder(bytes.NewReader(raw));decoder.DisallowUnknownFields()
    if decoder.Decode(&out)!=nil || decoder.Decode(new(any))!=io.EOF { return out,ErrRegionInvalid }
    if out.SchemaVersion!=1 || !regionExecutionRef(out.Source) || !regionExecutionRef(out.Style) || !validRegionSHA(out.SourceCanvasHash) || len(out.Stages)<1 || len(out.Stages)>128 { return out,ErrRegionInvalid }
    current:=out.Source;tasks:=map[uuid.UUID]bool{};results:=map[uuid.UUID]bool{}
    var previous *review.Canvas
    allowed:=map[string]bool{"geometry.body":true,"geometry.crop":true,"image.upscale":true,"image.denoise":true,"image.color":true,"image.local":true,"portrait.beauty":true,"portrait.detail":true,"series.balance":true,"sensor.dust_repair":true,"sensor.dust_auto_repair":true,"defects.repair":true,"defects.auto_repair":true,"background.compose":true,"background.blur":true,"image.inpaint":true,"image.generate":true,"image.outpaint":true}
    for _,stage:=range out.Stages {
        if !allowed[stage.Operation] || stage.OperationVersion!=1 || stage.TaskID==uuid.Nil || stage.ResultID==uuid.Nil || tasks[stage.TaskID] || results[stage.ResultID] || stage.Attempt<1 || stage.FencingToken<1 || stage.FencingToken>9007199254740991 || !regionExecutionRef(stage.Params) || !regionExecutionRef(stage.Output) || stage.Source!=current || stage.Output==stage.Source || stage.Params==stage.Source || stage.Params==stage.Output { return RegionExecutionDescriptor{},ErrRegionInvalid }
        if previous!=nil && *previous!=stage.SourceCanvas { return RegionExecutionDescriptor{},ErrRegionInvalid }
        if stage.InputMaskCount<0||stage.InputMaskCount>257{return RegionExecutionDescriptor{},ErrRegionInvalid}
        reviewed:=stage.Operation=="defects.repair"||stage.Operation=="sensor.dust_repair"
        if reviewed&&len(stage.ReviewAuthority)==0||string(stage.ReviewAuthority)=="null"{return RegionExecutionDescriptor{},ErrRegionInvalid}
        if !reviewed&&len(stage.ReviewAuthority)>0{var authority struct{Route string `json:"route"`};if json.Unmarshal(stage.ReviewAuthority,&authority)!=nil||authority.Route!="base"{return RegionExecutionDescriptor{},ErrRegionInvalid}}
        if stage.Operation=="image.generate"&&(stage.Phase!="background"&&stage.Phase!="generate")||stage.Operation=="image.outpaint"&&stage.Phase!="composition_render"||stage.Operation!="image.generate"&&stage.Operation!="image.outpaint"&&stage.Phase!=""{return RegionExecutionDescriptor{},ErrRegionInvalid}
        if (stage.Operation=="geometry.crop"||stage.Operation=="image.outpaint") && (stage.Plan==nil || stage.PolicyParams==nil || !regionExecutionRef(*stage.Plan) || !regionExecutionRef(*stage.PolicyParams)) {return RegionExecutionDescriptor{},ErrRegionInvalid}
        if stage.Operation=="geometry.body" && (stage.Report==nil || !regionExecutionRef(*stage.Report)) {return RegionExecutionDescriptor{},ErrRegionInvalid}
        if (stage.BackgroundAsset==nil)!=(stage.BackgroundParams==nil) || (stage.BackgroundAsset==nil)!=(stage.BackgroundImage==nil) || (stage.BackgroundAsset==nil)!=(stage.BackgroundCanvas==nil) || stage.BackgroundAsset!=nil && (stage.Operation!="background.compose"||!regionExecutionRef(*stage.BackgroundParams)||!regionExecutionRef(*stage.BackgroundImage)||stage.BackgroundCanvas.Width<1||stage.BackgroundCanvas.Height<1||stage.BackgroundCanvas.Width>200000000/stage.BackgroundCanvas.Height) {return RegionExecutionDescriptor{},ErrRegionInvalid}
        requires:=stage.Operation=="geometry.body" || stage.Operation=="geometry.crop" || stage.Operation=="image.upscale" || stage.Operation=="image.outpaint"
        if len(stage.Transform)==0 {
            if requires || stage.SourceCanvas!=stage.TargetCanvas { return RegionExecutionDescriptor{},ErrRegionInvalid }
        } else {
            var transform struct { Kind string `json:"kind"`; From review.Canvas `json:"from_canvas"`; To review.Canvas `json:"to_canvas"` }
            if json.Unmarshal(stage.Transform,&transform)!=nil || transform.From!=stage.SourceCanvas || transform.To!=stage.TargetCanvas { return RegionExecutionDescriptor{},ErrRegionInvalid }
            expected:=map[string]string{"geometry.body":"dense_warp","geometry.crop":"matrix","image.upscale":"resize","image.outpaint":"matrix"}
            if expected[stage.Operation]=="" || transform.Kind!=expected[stage.Operation] { return RegionExecutionDescriptor{},ErrRegionInvalid }
        }
        tasks[stage.TaskID]=true;results[stage.ResultID]=true;current=stage.Output
        target:=stage.TargetCanvas;previous=&target
    }
    return out,nil
}

func regionExecutionRef(ref v2input.Ref) bool { return ref.ArtifactID!=uuid.Nil && validRegionSHA(ref.SHA256) }

// freezeExecution adds only server-verified immutable evidence. It is private so
// public request decoding cannot install geometry plans or historical outputs.
// Callers must first independently verify SQL authority and all physical refs.
func (p FrozenRegionPlan) freezeExecution(execution RegionExecutionDescriptor,inputs []v2input.Entry) (FrozenRegionPlan,error) {
    document,err:=p.decode();if err!=nil{return FrozenRegionPlan{},err}
    raw,err:=canonical.Marshal(execution);if err!=nil{return FrozenRegionPlan{},err}
    owned,err:=decodeRegionExecution(raw);if err!=nil{return FrozenRegionPlan{},err}
    if owned.Source!=document.Source.Ref || owned.Style!=document.FrozenRecipe.BaseStyle || owned.SourceCanvasHash!=document.FrozenRecipe.Source.CanvasHash {return FrozenRegionPlan{},ErrRegionInvalid}
    if len(inputs)<4 || len(inputs)>511 {return FrozenRegionPlan{},ErrRegionInvalid}
    lookup:=map[uuid.UUID]v2input.Entry{}
    for _,entry:=range inputs {
        if !regionExecutionRef(entry.Ref) || entry.Contract=="" || len(entry.Contract)>120 {return FrozenRegionPlan{},ErrRegionInvalid}
        if _,exists:=lookup[entry.Ref.ArtifactID];exists{return FrozenRegionPlan{},ErrRegionInvalid}
        lookup[entry.Ref.ArtifactID]=entry
    }
    for _,entry:=range p.inputs {if lookup[entry.Ref.ArtifactID]!=entry{return FrozenRegionPlan{},ErrRegionInvalid}}
    for _,stage:=range owned.Stages {
        for _,ref:=range []v2input.Ref{stage.Source,stage.Output} {
            if lookup[ref.ArtifactID]!=(v2input.Entry{Ref:ref,Contract:"WorkingImage"}) {return FrozenRegionPlan{},ErrRegionInvalid}
        }
        for _,ref:=range []*v2input.Ref{stage.Plan,stage.PolicyParams} {if ref!=nil {entry,exists:=lookup[ref.ArtifactID];if !exists || entry.Ref!=*ref {return FrozenRegionPlan{},ErrRegionInvalid}}}
        if stage.Report!=nil {entry,exists:=lookup[stage.Report.ArtifactID];if !exists || entry.Ref!=*stage.Report {return FrozenRegionPlan{},ErrRegionInvalid}}
        if stage.BackgroundAsset!=nil{
            original:=v2input.Ref{ArtifactID:stage.BackgroundAsset.Source.ID,SHA256:stage.BackgroundAsset.Source.SHA256}
            if lookup[original.ArtifactID]!=(v2input.Entry{Ref:original,Contract:"EncodedImage"})||lookup[stage.BackgroundParams.ArtifactID]!=(v2input.Entry{Ref:*stage.BackgroundParams,Contract:"Params"})||lookup[stage.BackgroundImage.ArtifactID]!=(v2input.Entry{Ref:*stage.BackgroundImage,Contract:"WorkingImage"}){return FrozenRegionPlan{},ErrRegionInvalid}
        }
        entry,exists:=lookup[stage.Params.ArtifactID]
        if !exists || entry.Ref!=stage.Params || entry.Contract=="WorkingImage" || entry.Contract=="NpyArray" {return FrozenRegionPlan{},ErrRegionInvalid}
    }
    sorted:=append([]v2input.Entry(nil),inputs...)
    sort.Slice(sorted,func(i,j int)bool{return bytes.Compare(sorted[i].Ref.ArtifactID[:],sorted[j].Ref.ArtifactID[:])<0})
    document.Execution=&owned;document.Inputs=sorted
    body,err:=canonical.Marshal(document);if err!=nil{return FrozenRegionPlan{},err}
    digest,err:=canonical.Hash(document);if err!=nil{return FrozenRegionPlan{},err}
    return FrozenRegionPlan{body:body,digest:digest,inputs:append([]v2input.Entry(nil),sorted...),canvas:p.canvas,hasAlpha:p.hasAlpha},nil
}
