package v2input

import (
 "bytes"
 "encoding/json"
 "io"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)

// LensIntent selects one exact qualified profile for every frozen snapshot
// version. Resource budgets, actor, authentication session and policy are server
// authority and deliberately absent from this wire contract.
type LensIntent struct {
 SchemaVersion int `json:"schema_version"`
 SnapshotID uuid.UUID `json:"snapshot_id"`
 Choices []LensIntentChoice `json:"choices"`
}
type LensIntentChoice struct {
 AssetVersionID uuid.UUID `json:"asset_version_id"`
 Profile Ref `json:"profile"`
 Strength float64 `json:"strength"`
}

func DecodeLensIntent(raw []byte) (LensIntent,error) {
 var intent LensIntent
 if len(raw)<2 || len(raw)>1048576 { return intent,ErrInvalid }
 // Canonical parsing rejects duplicate keys before the typed decoder runs.
 if _,err:=canonical.Bytes(raw);err!=nil { return LensIntent{},ErrInvalid }
 var root map[string]json.RawMessage
 if json.Unmarshal(raw,&root)!=nil || len(root)!=3 || root["schema_version"]==nil || root["snapshot_id"]==nil || root["choices"]==nil { return LensIntent{},ErrInvalid }
 decoder:=json.NewDecoder(bytes.NewReader(raw));decoder.DisallowUnknownFields()
 if decoder.Decode(&intent)!=nil || decoder.Decode(new(any))!=io.EOF || intent.SchemaVersion!=1 || intent.SnapshotID==uuid.Nil || len(intent.Choices)<1 || len(intent.Choices)>5000 { return LensIntent{},ErrInvalid }
 var rows []map[string]json.RawMessage
 if json.Unmarshal(root["choices"],&rows)!=nil || len(rows)!=len(intent.Choices) { return LensIntent{},ErrInvalid }
 seen:=map[uuid.UUID]bool{}
 for i,choice:=range intent.Choices {
  row:=rows[i]
  var profile map[string]json.RawMessage
  if len(row)!=3 || row["asset_version_id"]==nil || row["strength"]==nil || bytes.Equal(bytes.TrimSpace(row["strength"]),[]byte("null")) || json.Unmarshal(row["profile"],&profile)!=nil || len(profile)!=2 || profile["artifact_id"]==nil || profile["sha256"]==nil || choice.AssetVersionID==uuid.Nil || seen[choice.AssetVersionID] || !validRef(choice.Profile) || !finiteRange(choice.Strength,0,1) { return LensIntent{},ErrInvalid }
  seen[choice.AssetVersionID]=true
 }
 return intent,nil
}

func (intent LensIntent) ProfileChoices() []LensProfileChoice {
 choices:=make([]LensProfileChoice,len(intent.Choices))
 for i,choice:=range intent.Choices { choices[i]=LensProfileChoice{AssetVersionID:choice.AssetVersionID,Profile:choice.Profile,Strength:choice.Strength} }
 return choices
}
