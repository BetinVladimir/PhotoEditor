package workflow

import (
 "encoding/json"
 "testing"
)
func TestEditorPrecomposePrecedesAllStyleStages(t *testing.T){
 stages:=[]ApplyV4Stage{{Phase:"resolve",Operation:"style.resolve",OutputRole:"resolved_style",Cardinality:"item"},{Phase:"color",Operation:"image.color",OutputRole:"working_image",Cardinality:"item"}}
 config:=json.RawMessage(`{"editor_precompose":{"original_recipe":{"artifact_id":"00000000-0000-4000-8000-000000000001","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"source":{"asset_version_id":"00000000-0000-4000-8000-000000000002"},"inputs":[{"artifact_id":"00000000-0000-4000-8000-000000000001","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}}`)
 got,err:=editorPrecomposeStages(config,stages);if err!=nil||len(got)!=3||got[0].Operation!="editor.layers_precompose"||got[1]!=stages[0]||got[2]!=stages[1]{t.Fatal("style or layers reordered",got,err)}
 again,err:=editorPrecomposeStages(config,got);if err!=nil||len(again)!=3{t.Fatal("precompose repeated",again,err)}
 ordinary,err:=editorPrecomposeStages(json.RawMessage(`{}`),stages);if err!=nil||len(ordinary)!=2||ordinary[0]!=stages[0]{t.Fatal("ordinary Apply changed")}
}
func TestEditorPrecomposeRejectsMissingFrozenOrigin(t *testing.T){
 for _,raw:=range []string{`{"editor_precompose":null}`,`{"editor_precompose":{}}`,`{"editor_precompose":{"inputs":[]}}`}{if _,err:=editorPrecomposeFromConfig(json.RawMessage(raw));err==nil{t.Fatal("missing server origin accepted",raw)}}
}
