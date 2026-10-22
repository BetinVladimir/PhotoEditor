package workflow

import (
 "context"
 "bytes"
 "encoding/json"
 "errors"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/review"
)

type editorPrecomposeConfig struct {
 OriginalRecipe review.ArtifactRef `json:"original_recipe"`
 SourceResult *review.ArtifactRef `json:"source_result"`
 Source json.RawMessage `json:"source"`
 Inputs []review.ArtifactRef `json:"inputs"`
 Profile json.RawMessage `json:"execution_profile"`
}
func editorPrecomposeFromConfig(raw json.RawMessage)(*editorPrecomposeConfig,error){
 var fields map[string]json.RawMessage;if json.Unmarshal(raw,&fields)!=nil{return nil,errors.New("invalid editor source config")}
 value,ok:=fields["editor_precompose"];if !ok{return nil,nil}
 var out editorPrecomposeConfig;if json.Unmarshal(value,&out)!=nil||out.OriginalRecipe.ID.String()=="00000000-0000-0000-0000-000000000000"||len(out.Source)==0||len(out.Inputs)<1||len(out.Inputs)>4096{return nil,errors.New("invalid editor precompose config")};return &out,nil
}
func editorPrecomposeStages(config json.RawMessage,stages []ApplyV4Stage)([]ApplyV4Stage,error){
 mode,err:=editorPrecomposeFromConfig(config);if err!=nil||mode==nil{return stages,err}
 if len(stages)>0&&stages[0].Phase=="editor_precompose"{return stages,nil}
 return append([]ApplyV4Stage{{Phase:"editor_precompose",Operation:"editor.layers_precompose",OutputRole:"working_image",Cardinality:"item"}},stages...),nil
}
func(c ApplyCoordinator)buildEditorPrecompose(ctx context.Context,l ApplyLease,reg Registry,items []applyInput,plan []ApplyV4Stage)(bool,error){
 if len(items)!=1||len(plan)<2||plan[0].Phase!="editor_precompose"{return false,errors.New("invalid editor precompose stage")}
 item:=items[0];mode,err:=editorPrecomposeFromConfig(item.Config);if err!=nil||mode==nil{return false,errors.New("editor precompose origin missing")}
 raw,err:=canonical.Marshal(map[string]any{"schema_version":1,"build_run_id":l.RunID,"original_recipe":mode.OriginalRecipe,"source":mode.Source,"source_result":mode.SourceResult});if err!=nil{return false,err}
 hash,err:=canonical.Hash(json.RawMessage(raw));if err!=nil{return false,err}
 id:=nodeID(l.RunID,"editor_precompose/"+item.ItemID.String())
 profile:=append(json.RawMessage(nil),mode.Profile...)
 if !editorPrecomposeHasPin(profile){
  var settled bool
  settled,err=c.Store.FailEditorPrecompose(ctx,l)
  if err!=nil{return false,err};if !settled{return false,errors.New("editor precompose profile failure authority stale")};return true,nil
 }
 node:=applyNodePlan{ID:id,Phase:"editor_precompose",Ordinal:item.Ordinal,SourceItemID:item.ItemID,SourceAssetID:item.AssetID,SourceVersionID:item.VersionID,NodeKey:"editor_precompose/"+item.ItemID.String(),Operation:"editor.layers_precompose",Inputs:mode.Inputs,Prerequisites:[]uuid.UUID{},ParamsSHA:hash,Params:raw}
 if err=c.materializeNode(ctx,l,reg,node,profile);err!=nil{return false,err}
 return c.Store.Advance(ctx,l,plan[1].Phase)
}
// Every later stage reads the exact task-owned precomposition, while snapshot
// identity and source-version provenance remain the immutable original input.
func(c ApplyCoordinator)editorEffectInputs(ctx context.Context,l ApplyLease,items []applyInput)([]applyInput,error){
 if len(items)!=1{return nil,errors.New("editor single source cardinality changed")}
 outputs,err:=c.Store.Outputs(ctx,l,"editor.layers_precompose","working_image");if err!=nil||len(outputs)!=1{return nil,errors.New("editor precompose canonical output missing")}
 if outputs[0].ItemID!=items[0].ItemID{return nil,errors.New("editor precompose source changed")}
 working,err:=readWorking(ctx,c.Objects,c.Bucket,outputs[0].Artifact);if err!=nil{return nil,err}
 if working.Canvas!=items[0].Working.Canvas||working.Color!=items[0].Working.Color||!bytes.Equal(working.TransformChain,items[0].Working.TransformChain){return nil,errors.New("editor precompose changed source geometry")}
 canvas,err:=canonical.Hash(working.Canvas);if err!=nil{return nil,err};var recorded bool
 if recorded,err=c.Store.RecordEditorPrecompose(ctx,l,outputs[0].Artifact.ref(),canvas);err!=nil{return nil,err};if !recorded{return nil,errors.New("editor precompose origin recording rejected")}
 items[0].Image=outputs[0].Artifact;items[0].Working=working;return items,nil
}

// Legacy accepted profiles must not be made executable by inferring pins.
// Mirror the narrow SQL recovery predicate, including null/scalar/empty values.
func editorPrecomposeHasPin(profile json.RawMessage)bool{
 var fields map[string]json.RawMessage
 if json.Unmarshal(profile,&fields)!=nil{return false}
 var pin map[string]string
 if json.Unmarshal(fields["native_qualification_pin"],&pin)!=nil||len(pin)!=6||pin["backend"]!="cpu"{return false}
 for _,name:=range []string{"model_lock_sha","runtime_lock_sha","device_profile_sha","dataset_sha","calibration_sha"}{if _,err:=domain.ParseDigest(pin[name]);err!=nil{return false}}
 return true
}
