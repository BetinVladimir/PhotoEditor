package workflow

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// projectDeliverySources follows physical JSON references only within the
// SQL-authorized parent inventory. It cannot promote a document-supplied ref to
// authorization. Shared dependencies are emitted once; cycles and missing or
// substituted refs are refused. Binary leaves are verified later by the worker.
func projectDeliverySources(ctx context.Context, roots []v2input.Ref, metadata map[uuid.UUID]deliverySourceEntry, read func(context.Context, deliverySourceEntry) ([]byte, error)) ([]v2input.Entry, error) {
	state := map[uuid.UUID]int{}
	result := []v2input.Entry{}
	var total int64
	var visit func(v2input.Ref, int) error
	visit = func(ref v2input.Ref, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		item, ok := metadata[ref.ArtifactID]
		if !ok || item.Ref != ref || depth > 64 {
			return v2input.ErrInvalid
		}
		if state[ref.ArtifactID] == 1 {
			return v2input.ErrInvalid
		}
		if state[ref.ArtifactID] == 2 {
			return nil
		}
		if len(state) >= 4096 {
			return v2input.ErrInvalid
		}
		state[ref.ArtifactID] = 1
		if item.MediaType == "application/json" {
			if item.Bytes < 2 || item.Bytes > 8<<20 || total > 64<<20-item.Bytes {
				return v2input.ErrInvalid
			}
			total += item.Bytes
			body, err := read(ctx, item)
			if err != nil {
				return err
			}
			// The reader must verify physical SHA/size/canonical JSON. Repeat SHA/size
			// here so a future reader replacement cannot redefine the inventory.
			if int64(len(body)) != item.Bytes || renderDerivativeBytesSHA(body) != ref.SHA256 {
				return v2input.ErrInvalid
			}
			refs, err := deliveryJSONRefs(body)
			if err != nil {
				return err
			}
			for _, child := range refs {
				if err := visit(child, depth+1); err != nil {
					return err
				}
			}
		}
		state[ref.ArtifactID] = 2
		result = append(result, v2input.Entry{Ref: ref, Contract: item.Contract})
		return nil
	}
	for _, root := range roots {
		if err := visit(root, 0); err != nil {
			return nil, err
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Ref.ArtifactID.String() < result[j].Ref.ArtifactID.String() })
	return result, nil
}

func deliveryJSONRefs(raw []byte) ([]v2input.Ref, error) {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil, v2input.ErrInvalid
	}
	out := []v2input.Ref{}
	var walk func(any, int) error
	walk = func(value any, depth int) error {
		if depth > 64 {
			return v2input.ErrInvalid
		}
		switch node := value.(type) {
		case map[string]any:
			id, hasArtifact := node["artifact_id"]
			sha, hasSHA := node["sha256"]
			if !hasArtifact && hasSHA {
				id, hasArtifact = node["id"]
			}
			if hasArtifact {
				idText, idOK := id.(string)
				shaText, shaOK := sha.(string)
				parsed, err := uuid.Parse(idText)
				if !idOK || !hasSHA || !shaOK || err != nil || parsed == uuid.Nil || len(shaText) != 64 {
					return v2input.ErrInvalid
				}
				for _, c := range shaText {
					if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
						return v2input.ErrInvalid
					}
				}
				out = append(out, v2input.Ref{ArtifactID: parsed, SHA256: shaText})
				if len(out) > 1000000 {
					return v2input.ErrInvalid
				}
			}
			keys := make([]string, 0, len(node))
			for key := range node {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				if err := walk(node[key], depth+1); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range node {
				if err := walk(child, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(value, 0); err != nil {
		return nil, err
	}
	return out, nil
}
