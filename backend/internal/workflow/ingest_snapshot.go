package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

// IngestSnapshotPayload is the complete versioned per-file snapshot contract.
// V1 retains its exact legacy shape. V2 additionally requires RAW development.
type IngestSnapshotPayload struct {
	SchemaVersion int     `json:"schema_version"`
	Suffix        string  `json:"suffix"`
	AssumeProfile *string `json:"assume_profile"`
	Analyze       bool    `json:"analyze"`
	Tile          int     `json:"tile"`
	RAWDevelopment *RAWDevelopment `json:"raw_development,omitempty"`
}

type IngestRunContext struct {
	Phase string
	Spec  IngestSpec
}

type IngestRunReader struct{ DB ActivationRows }

// Load reads a single database snapshot. Mutators must still recheck phase under
// their own locks; this read is neither a lease nor authorization to change state.
func (r IngestRunReader) Load(ctx context.Context, tenant, run uuid.UUID) (IngestRunContext, error) {
	invalid := errors.New("invalid persisted ingest snapshot")
	fail := func() (IngestRunContext, error) { return IngestRunContext{}, invalid }
	if r.DB == nil || tenant == uuid.Nil || run == uuid.Nil {
		return fail()
	}
	rows, err := r.DB.Query(ctx, "SELECT phase,graph_sealed,item_count,ordinal,source_id,source_sha,source_bytes,payload FROM photo.ingest_run_context($1,$2)", tenant, run)
	if err != nil {
		return IngestRunContext{}, err
	}
	defer rows.Close()
	out := IngestRunContext{Spec: IngestSpec{RunID: run}}
	var expected int64
	seen := map[int64]bool{}
	for rows.Next() {
		var phase string
		var sealed bool
		var count int64
		var item IngestItem
		var raw []byte
		if err = rows.Scan(&phase, &sealed, &count, &item.Ordinal, &item.Source.ID, &item.Source.SHA256, &item.Bytes, &raw); err != nil {
			return IngestRunContext{}, err
		}
		if count < 1 || count > 10000 || len(out.Spec.Items) >= int(count) || seen[item.Ordinal] {
			return fail()
		}
		switch phase {
		case "planning", "decode_build":
			if sealed {
				return fail()
			}
		case "inspection", "decode", "catalog":
			if !sealed {
				return fail()
			}
		default:
			return fail()
		}
		if len(out.Spec.Items) == 0 {
			out.Phase = phase
			expected = count
		} else if out.Phase != phase || expected != count {
			return fail()
		}
		if len(raw) > 4096 {
			return fail()
		}
		normalized, e := canonical.Bytes(raw)
		if e != nil {
			return fail()
		}
		var payload IngestSnapshotPayload
		dec := json.NewDecoder(bytes.NewReader(normalized))
		dec.DisallowUnknownFields()
		if dec.Decode(&payload) != nil {
			return fail()
		}
		roundtrip, e := canonical.Marshal(payload)
		if e != nil || !bytes.Equal(normalized, roundtrip) || (payload.SchemaVersion != 1 && payload.SchemaVersion != 2) || (payload.Tile != 256 && payload.Tile != 512 && payload.Tile != 1024) {
			return fail()
		}
        if payload.SchemaVersion == 1 && payload.RAWDevelopment != nil ||
           payload.SchemaVersion == 2 && payload.RAWDevelopment == nil ||
           !validRAWDevelopment(payload.RAWDevelopment) { return fail() }
        item.RAWDevelopment = payload.RAWDevelopment
		item.Suffix = payload.Suffix
		item.AssumeProfile = payload.AssumeProfile
		item.Analyze = payload.Analyze
		item.Tile = payload.Tile
		out.Spec.Items = append(out.Spec.Items, item)
		seen[item.Ordinal] = true
	}
	if err = rows.Err(); err != nil {
		return IngestRunContext{}, err
	}
	if expected == 0 || int64(len(out.Spec.Items)) != expected {
		return fail()
	}
	registry, err := LoadRegistry()
	if err != nil {
		return IngestRunContext{}, err
	}
	if _, err = CompileIngest(out.Spec, registry); err != nil {
		return fail()
	}
	sort.Slice(out.Spec.Items, func(i, j int) bool { return out.Spec.Items[i].Ordinal < out.Spec.Items[j].Ordinal })
	return out, nil
}
