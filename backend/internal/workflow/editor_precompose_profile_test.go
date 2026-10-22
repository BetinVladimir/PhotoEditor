package workflow

import (
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2render"
)

func TestEditorPrecomposeRetainsFrozenBudgetRatherThanOldLinearEstimate(t *testing.T){
 // Same 16x16/sample32x64/ceil1.2 result as the Python calibration selector
 // regression. This is a source-authored accounting contract, not a measured
 // provider fixture or a physical qualification claim.
 q:=v2render.RenderQualification{Backend:"cpu",ModelSHA:repeatProfilePin("a"),RuntimeSHA:repeatProfilePin("b"),DeviceSHA:repeatProfilePin("c"),DatasetSHA:repeatProfilePin("d")}
 cal:=v2render.RenderCalibration{Qualification:q,EvidenceSHA:repeatProfilePin("f"),ICCSHA:repeatProfilePin("0"),TileSize:512,AlphaMode:"require_float32",Samples:[]v2render.RenderCalibrationSample{{Width:32,Height:64,Provider:v2render.RenderProviderPeak{RAMBytes:100,ScratchBytes:10,OutputBytes:11}}}}
 choice,e:=cal.Select(q,16,16,cal.ICCSHA);if e!=nil{t.Fatal(e)}
 if choice.Provider!=(v2render.RenderProviderPeak{RAMBytes:100,ScratchBytes:2,OutputBytes:2}){t.Fatal(choice.Provider)}
 budget,e:=v2render.RenderResources(16,16,0,0,choice.TileSize,choice.AlphaMode,choice.Provider);if e!=nil{t.Fatal(e)}
 if budget.RAMBytes<=16*16*64+(128<<20)||budget.OutputBytes<=16*16*24+(10<<20){t.Fatal("old underestimated resource profile returned")}
 profile,e:=canonical.Marshal(map[string]any{"backend":"cpu","tile_size":512,"alpha_mode":"require_float32","resource_estimate":budget,"native_qualification_pin":q});if e!=nil{t.Fatal(e)}
 raw,e:=canonical.Marshal(map[string]any{"editor_precompose":map[string]any{"original_recipe":map[string]any{"artifact_id":uuid.New(),"sha256":repeatProfilePin("a")},"source":map[string]any{"test":"retained"},"inputs":[]any{map[string]any{"artifact_id":uuid.New(),"sha256":repeatProfilePin("b")}},"execution_profile":json.RawMessage(profile)}});if e!=nil{t.Fatal(e)}
 parsed,e:=editorPrecomposeFromConfig(raw);if e!=nil||parsed==nil{t.Fatal(e)}
 got,_:=canonical.Bytes(parsed.Profile);want,_:=canonical.Bytes(profile);if string(got)!=string(want){t.Fatal("frozen CPU costs changed during coordinator decoding")}
}
func repeatProfilePin(v string)string{out:="";for i:=0;i<64;i++{out+=v};return out}

func TestEditorPrecomposeMissingOrMalformedPinHasOneRecoveryClassification(t *testing.T){
 pin:=map[string]string{"backend":"cpu","model_lock_sha":repeatProfilePin("a"),"runtime_lock_sha":repeatProfilePin("b"),"device_profile_sha":repeatProfilePin("c"),"dataset_sha":repeatProfilePin("d"),"calibration_sha":repeatProfilePin("e")}
 good,_:=canonical.Marshal(map[string]any{"native_qualification_pin":pin})
 if !editorPrecomposeHasPin(good){t.Fatal("valid pin refused")}
 for _,raw:=range []string{`null`,`{}`,`{"native_qualification_pin":null}`,`{"native_qualification_pin":"broken"}`,`{"native_qualification_pin":{}}`}{if editorPrecomposeHasPin([]byte(raw)){t.Fatal("legacy/corrupt pin executable",raw)}}
 pin["backend"]="cuda";foreign,_:=canonical.Marshal(map[string]any{"native_qualification_pin":pin});if editorPrecomposeHasPin(foreign){t.Fatal("CPU stage borrowed GPU pin")}
}
