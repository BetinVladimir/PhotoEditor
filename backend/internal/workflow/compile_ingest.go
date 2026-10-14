package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

type IngestItem struct {
	Ordinal       int64
	Source        review.ArtifactRef
	Bytes         int64
	Suffix        string
	AssumeProfile *string
	Analyze       bool
	Tile          int
	RAWDevelopment *RAWDevelopment
}
type IngestSpec struct {
	RunID uuid.UUID
	Items []IngestItem
}
type CompiledIngestNode struct {
	ID                                     uuid.UUID
	Key, Operation, Version, ResourceClass string
	TimeoutSeconds                         int
	Inputs                                 []review.ArtifactRef
	Params                                 json.RawMessage
}
type IngestPlan struct {
	Graph Graph
	Nodes []CompiledIngestNode
}
type ingestSourceParams struct {
	Source review.ArtifactRef `json:"source"`
	Bytes  int64              `json:"expected_bytes"`
	Suffix string             `json:"suffix"`
}
type ingestDecodeParams struct {
	ingestSourceParams
	AssumeProfile *string `json:"assume_profile"`
	Analyze       bool    `json:"analyze"`
	Tile          int     `json:"tile"`
	RAWDevelopment *RAWDevelopment `json:"raw_development,omitempty"`
}

// CompileIngest emits the complete verify/inspect/decode DAG for a snapshot
// batch. It does not publish params, grant worker access, or claim qualification.
// Ordinals originate in the immutable snapshot; identical content at distinct
// ordinals remains distinct work. Recompilation is independent of input order.
func CompileIngest(spec IngestSpec, registry Registry) (IngestPlan, error) {
	fail := func() (IngestPlan, error) { return IngestPlan{}, errors.New("invalid ingest specification") }
	if spec.RunID == uuid.Nil || len(spec.Items) == 0 || len(spec.Items) > 10000 {
		return fail()
	}
	items := append([]IngestItem(nil), spec.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].Ordinal < items[j].Ordinal })
	suffixes := map[string]bool{}
	for _, s := range strings.Fields(".jpg .jpeg .png .tif .tiff .nef .nrw .crw .cr2 .cr3 .arw .srf .sr2 .mrw .dng .rw2 .orf .raf .pef .srw .3fr .fff .iiq .mos .kdc .mef .x3f") {
		suffixes[s] = true
	}
	out := IngestPlan{Graph: Graph{Nodes: []Node{}, Edges: []Edge{}}, Nodes: []CompiledIngestNode{}}
	for i, item := range items {
		if item.Ordinal < 0 || item.Ordinal > 9007199254740991 || (i > 0 && item.Ordinal == items[i-1].Ordinal) || !validExpansionRef(item.Source) || item.Bytes < 1 || item.Bytes > 536870912 || !suffixes[item.Suffix] {
			return fail()
		}
		if item.AssumeProfile != nil {
			switch *item.AssumeProfile {
			case "srgb_v1", "linear_srgb_v1", "work_rec2020_gamma24_v1":
			default:
				return fail()
			}
		}
		tile := item.Tile
		if tile == 0 {
			tile = 512
		}
		if tile != 256 && tile != 512 && tile != 1024 {
			return fail()
		}
        if !validRAWDevelopment(item.RAWDevelopment) { return fail() }
        var development *RAWDevelopment
        if item.RAWDevelopment != nil {
            if item.Suffix == ".jpg" || item.Suffix == ".jpeg" || item.Suffix == ".png" || item.Suffix == ".tif" || item.Suffix == ".tiff" { return fail() }
            copy := *item.RAWDevelopment
            development = &copy
        }
		source := ingestSourceParams{Source: item.Source, Bytes: item.Bytes, Suffix: item.Suffix}
		sourceRaw, err := canonical.Marshal(source)
		if err != nil {
			return fail()
		}
		decodeRaw, err := canonical.Marshal(ingestDecodeParams{source, item.AssumeProfile, item.Analyze, tile, development})
		if err != nil {
			return fail()
		}
		previous := ""
		for _, name := range []string{"asset.verify", "asset.inspect", "asset.decode"} {
			op, ok := registry[name]
			if !ok || op.Name != name || op.Version != "1" || op.TimeoutSeconds < 1 || op.TimeoutSeconds > 7200 {
				return fail()
			}
			expectedClass := "cpu"
			if name == "asset.verify" {
				expectedClass = "media"
			}
			if op.ResourceClass != expectedClass {
				return fail()
			}
			cpu := false
			for _, backend := range op.Backends {
				if backend == "cpu" {
					cpu = true
				}
			}
			if !cpu {
				return fail()
			}
			key := fmt.Sprintf("%s/%016d", name, item.Ordinal)
			params := sourceRaw
			if name == "asset.decode" {
				params = decodeRaw
			}
			out.Graph.Nodes = append(out.Graph.Nodes, Node{Key: key, Operation: name})
			out.Nodes = append(out.Nodes, CompiledIngestNode{ID: uuid.NewSHA1(spec.RunID, []byte(key)), Key: key, Operation: name, Version: op.Version, ResourceClass: op.ResourceClass, TimeoutSeconds: op.TimeoutSeconds, Inputs: []review.ArtifactRef{item.Source}, Params: append(json.RawMessage(nil), params...)})
			if previous != "" {
				out.Graph.Edges = append(out.Graph.Edges, Edge{From: previous, To: key, Condition: "success"})
			}
			previous = key
		}
	}
	if _, err := out.Graph.Order(registry); err != nil {
		return IngestPlan{}, err
	}
	return out, nil
}
