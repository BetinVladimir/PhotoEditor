package v2region

import (
    "context"
    "encoding/json"
    "github.com/google/uuid"
    "photoeditor/backend/internal/canonical"
    "photoeditor/backend/internal/review"
    "photoeditor/backend/internal/v2input"
)

// RegionExecutionRecord is loaded only through the current canonical SQL
// candidate/document readers. Physical bodies remain independently verified.
type RegionExecutionRecord struct {
    Stage RegionExecutionStage
    ParamsBody []byte
    OutputBody []byte
}

// compileRegionExecution orders an authenticated run's image-producing DAG by
// actual Params.image, not timestamps/task UUIDs. Other batch items are ignored;
// ambiguous branches reaching the selected source fail rather than guessing.
func compileRegionExecution(ctx context.Context,sourceRef,targetRef,styleRef v2input.Ref,source review.WorkingImage,records []RegionExecutionRecord) (RegionExecutionDescriptor,error) {
    empty:=RegionExecutionDescriptor{}
    if len(records)<1 || len(records)>128 || !regionExecutionRef(sourceRef) || !regionExecutionRef(targetRef) { return empty,ErrRegionInvalid }
    sourceHash,err:=canonical.Hash(source.Canvas);if err!=nil{return empty,err}
    current:=sourceRef;image:=source;used:=map[uuid.UUID]bool{}
    descriptor:=RegionExecutionDescriptor{SchemaVersion:1,Source:sourceRef,SourceCanvasHash:sourceHash,Style:styleRef}
    for current!=targetRef {
        if err:=ctx.Err();err!=nil{return empty,err}
        matches:=[]RegionExecutionRecord{}
        for _,record:=range records {
            if used[record.Stage.TaskID] {continue}
            params,err:=canonical.Bytes(record.ParamsBody)
            hash,hashErr:=canonical.Hash(json.RawMessage(record.ParamsBody))
            if err!=nil || hashErr!=nil || string(params)!=string(record.ParamsBody) || hash!=record.Stage.Params.SHA256 {return empty,ErrRegionInvalid}
            var binding struct {Image v2input.Ref `json:"image"`}
            if json.Unmarshal(record.ParamsBody,&binding)!=nil{return empty,ErrRegionInvalid}
            if binding.Image==current {matches=append(matches,record)}
        }
        if len(matches)!=1 {return empty,ErrRegionInvalid}
        record:=matches[0];stage:=record.Stage
        outputCanonical,canonicalErr:=canonical.Bytes(record.OutputBody)
        hash,err:=canonical.Hash(json.RawMessage(record.OutputBody));if canonicalErr!=nil || string(outputCanonical)!=string(record.OutputBody) || err!=nil || hash!=stage.Output.SHA256{return empty,ErrRegionInvalid}
        target,err:=review.ParseWorkingImage(record.OutputBody);if err!=nil{return empty,ErrRegionInvalid}
        if image.Color!=target.Color {return empty,ErrRegionInvalid}
        var before,after struct {Steps []json.RawMessage `json:"steps"`}
        if json.Unmarshal(image.TransformChain,&before)!=nil || json.Unmarshal(target.TransformChain,&after)!=nil{return empty,ErrRegionInvalid}
        if len(after.Steps)<len(before.Steps) || len(after.Steps)>len(before.Steps)+1{return empty,ErrRegionInvalid}
        for index:=range before.Steps {
            a,e1:=canonical.Bytes(before.Steps[index]);b,e2:=canonical.Bytes(after.Steps[index])
            if e1!=nil || e2!=nil || string(a)!=string(b){return empty,ErrRegionInvalid}
        }
        stage.Source=current;stage.SourceCanvas=image.Canvas;stage.TargetCanvas=target.Canvas;stage.TargetHasAlpha=target.Alpha!=nil
        if len(after.Steps)>len(before.Steps){stage.Transform=append(json.RawMessage(nil),after.Steps[len(before.Steps)]...)}else{stage.Transform=nil}
        descriptor.Stages=append(descriptor.Stages,stage);used[stage.TaskID]=true
        image=target;current=stage.Output
        if len(descriptor.Stages)>128{return empty,ErrRegionInvalid}
    }
    raw,err:=canonical.Marshal(descriptor);if err!=nil{return empty,err}
    return decodeRegionExecution(raw)
}
