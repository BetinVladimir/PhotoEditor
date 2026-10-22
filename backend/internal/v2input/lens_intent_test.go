package v2input

import (
 "strings"
 "testing"
 "github.com/google/uuid"
)

func TestLensIntentStrictFrozenBatch(t *testing.T) {
 raw:=`{"schema_version":1,"snapshot_id":"00000000-0000-4000-8000-000000000001","choices":[{"asset_version_id":"00000000-0000-4000-8000-000000000002","profile":{"artifact_id":"00000000-0000-4000-8000-000000000003","sha256":"`+strings.Repeat("a",64)+`"},"strength":0}]}`
 intent,err:=DecodeLensIntent([]byte(raw))
 if err!=nil || len(intent.ProfileChoices())!=1 || intent.ProfileChoices()[0].AssetVersionID==uuid.Nil || intent.ProfileChoices()[0].ProfileJSON!=nil { t.Fatal("valid frozen choice rejected",err) }
 for name,body:=range map[string]string{
  "missing strength":strings.Replace(raw,`,"strength":0`,"",1),
  "null strength":strings.Replace(raw,`"strength":0`,`"strength": null `,1),
  "case alias":strings.Replace(raw,`"snapshot_id"`,`"Snapshot_ID"`,1),
  "unknown budget":strings.Replace(raw,`"schema_version":1`,`"schema_version":1,"ram_bytes":100`,1),
  "duplicate key":strings.Replace(raw,`"schema_version":1`,`"schema_version":1,"schema_version":1`,1),
  "out of range":strings.Replace(raw,`"strength":0`,`"strength":1.1`,1),
  "trailing":raw+`{}`,
  "profile bytes injection":strings.Replace(raw,`"strength":0`,`"strength":0,"profile_json":{}`,1),
 } { t.Run(name,func(t *testing.T){if _,err:=DecodeLensIntent([]byte(body));err==nil { t.Fatal("invalid intent accepted") }}) }
 choices:=intent.ProfileChoices();choices[0].Strength=1
 if intent.Choices[0].Strength!=0 { t.Fatal("choice conversion aliases immutable intent") }
}
