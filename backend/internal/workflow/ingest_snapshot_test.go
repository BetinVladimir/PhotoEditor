package workflow

import (
	"context"
 "encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func snapshotValues(t *testing.T) (IngestSpec, [][]any) {
	t.Helper()
	spec, _ := ingestFixture(t)
	values := [][]any{}
	for i := len(spec.Items) - 1; i >= 0; i-- {
		item := spec.Items[i]
		tile := item.Tile
		if tile == 0 {
			tile = 512
		}
		raw, err := canonical.Marshal(IngestSnapshotPayload{1, item.Suffix, item.AssumeProfile, item.Analyze, tile, nil})
		if err != nil {
			t.Fatal(err)
		}
		values = append(values, []any{"planning", false, int64(len(spec.Items)), item.Ordinal, item.Source.ID, item.Source.SHA256, item.Bytes, raw})
	}
	return spec, values
}

func TestIngestRunReaderValidatesSnapshotAndPhase(t *testing.T) {
	for _, phase := range []string{"planning", "inspection", "decode_build", "decode", "catalog"} {
		t.Run(phase, func(t *testing.T) {
			spec, values := snapshotValues(t)
			for _, v := range values {
				v[0] = phase
				v[1] = phase != "planning" && phase != "decode_build"
			}
			rows := &outcomeRows{values: values}
			got, err := (IngestRunReader{DB: outcomeDB{rows}}).Load(context.Background(), uuid.New(), spec.RunID)
			if err != nil || got.Phase != phase || len(got.Spec.Items) != len(spec.Items) || !rows.closed {
				t.Fatal("snapshot read failed", err)
			}
			for i := 1; i < len(got.Spec.Items); i++ {
				if got.Spec.Items[i-1].Ordinal >= got.Spec.Items[i].Ordinal {
					t.Fatal("not sorted")
				}
			}
		})
	}
}

func TestIngestRunReaderRejectsIncompleteOrChangedSnapshot(t *testing.T) {
	tests := map[string]func([][]any) [][]any{
		"empty":               func(v [][]any) [][]any { return nil },
		"missing":             func(v [][]any) [][]any { return v[:len(v)-1] },
		"duplicate":           func(v [][]any) [][]any { v[1] = v[0]; return v },
		"mixed phase":         func(v [][]any) [][]any { v[1][0] = "decode_build"; return v },
		"count mismatch":      func(v [][]any) [][]any { v[1][2] = int64(999); return v },
		"sealed planning":     func(v [][]any) [][]any { v[0][1] = true; return v },
		"unsealed inspection": func(v [][]any) [][]any { v[0][0] = "inspection"; return v },
		"terminal":            func(v [][]any) [][]any { v[0][0] = "complete"; return v },
		"bad source":          func(v [][]any) [][]any { v[0][5] = "bad"; return v },
		"empty source":        func(v [][]any) [][]any { v[0][6] = int64(0); return v },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			spec, values := snapshotValues(t)
			rows := &outcomeRows{values: mutate(values)}
			if _, err := (IngestRunReader{DB: outcomeDB{rows}}).Load(context.Background(), uuid.New(), spec.RunID); err == nil || !rows.closed {
				t.Fatal("invalid snapshot accepted or rows leaked")
			}
		})
	}
}

func TestIngestSnapshotPayloadStrictContract(t *testing.T) {
	payloads := []string{
		`{}`, // required fields cannot silently default
		`{"schema_version":1,"suffix":".jpg","analyze":false,"tile":512}`,
		`{"schema_version":1,"suffix":".jpg","assume_profile":null,"analyze":null,"tile":512}`,
		`{"schema_version":2,"suffix":".jpg","assume_profile":null,"analyze":false,"tile":512}`,
		`{"schema_version":1,"suffix":".jpg","assume_profile":null,"analyze":false,"tile":0}`,
		`{"schema_version":1,"suffix":".jpg","assume_profile":"unknown","analyze":false,"tile":512}`,
		`{"schema_version":1,"suffix":".exe","assume_profile":null,"analyze":false,"tile":512}`,
		`{"schema_version":1,"suffix":".jpg","assume_profile":null,"analyze":false,"tile":512,"extra":true}`,
		`{"schema_version":1,"suffix":".jpg","assume_profile":null,"analyze":false,"analyze":true,"tile":512}`,
	}
	for _, payload := range payloads {
		t.Run(payload, func(t *testing.T) {
			spec, values := snapshotValues(t)
			values[0][7] = []byte(payload)
			rows := &outcomeRows{values: values}
			if _, err := (IngestRunReader{DB: outcomeDB{rows}}).Load(context.Background(), uuid.New(), spec.RunID); err == nil || !rows.closed {
				t.Fatal("invalid payload accepted")
			}
		})
	}
}

func TestIngestSnapshotV2RetainsExactRAWPolicy(t *testing.T) {
 settings:=RAWDevelopment{1,"dcb","daylight",1.5,"blend","rawpy-0.27.1/libraw-0.21.4"}
 for _,version:=range []int{1,2} {
  spec,values:=snapshotValues(t)
  // Fixture stores the RAW item first (ordinal2), then the PNG item.
  item:=spec.Items[1]
  raw,err:=canonical.Marshal(IngestSnapshotPayload{version,item.Suffix,item.AssumeProfile,item.Analyze,512,&settings})
  if err!=nil {t.Fatal(err)}
  values[0][7]=raw
  got,err:=(IngestRunReader{DB:outcomeDB{&outcomeRows{values:values}}}).Load(context.Background(),uuid.New(),spec.RunID)
  if version==1 {
   if err==nil {t.Fatal("RAW policy accepted in historical v1 shape")}
   continue
  }
  if err!=nil || got.Spec.Items[0].RAWDevelopment==nil || *got.Spec.Items[0].RAWDevelopment!=settings {t.Fatal("RAW snapshot policy lost",err)}
  _,registry:=ingestFixture(t)
  plan,err:=CompileIngest(got.Spec,registry)
  if err!=nil {t.Fatal(err)}
  found:=false
  for _,node:=range plan.Nodes {
   if node.Operation=="asset.decode" && node.Key=="asset.decode/0000000000000002" {
    var params ingestDecodeParams
    if err:=json.Unmarshal(node.Params,&params);err!=nil {t.Fatal(err)}
    if params.RAWDevelopment==nil || *params.RAWDevelopment!=settings {t.Fatal("snapshot-to-task policy changed")}
    found=true
   }
  }
  if !found {t.Fatal("RAW decode absent")}
 }
}

func TestIngestSnapshotV2RejectsPolicyOnRasterOrInvalidPolicy(t *testing.T) {
 for _,kind:=range []string{"raster","invalid version","invalid wb","invalid decoder"} {
  spec,values:=snapshotValues(t)
  item:=spec.Items[1]
  settings:=RAWDevelopment{1,"ahd","camera",0,"clip","rawpy-0.27.1/libraw-0.21.4"}
  switch kind {
  case "raster":item.Suffix=".png"
  case "invalid version":settings.SchemaVersion=2
  case "invalid wb":settings.WBSource="guess"
  case "invalid decoder":settings.DecoderVersion="unlocked"
  }
  raw,err:=canonical.Marshal(IngestSnapshotPayload{2,item.Suffix,item.AssumeProfile,item.Analyze,512,&settings})
  if err!=nil{t.Fatal(err)}
  values[0][7]=raw
  rows:=&outcomeRows{values:values}
  if _,err=(IngestRunReader{DB:outcomeDB{rows}}).Load(context.Background(),uuid.New(),spec.RunID);err==nil || !rows.closed {t.Fatal("invalid RAW snapshot accepted",kind)}
 }
}
