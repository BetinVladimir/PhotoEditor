package v2draft

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strconv"
)

// Authored transcript parsing grants no catalog or producer authority. Opaque
// output observations never substitute for explicit typed root occurrences.
type authoredFields map[string]json.RawMessage

func authoredObject(raw []byte, required string, optional ...string) (authoredFields, error) {
	wire, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(wire, raw) {
		return nil, v2input.ErrInvalid
	}
	var fields authoredFields
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, v2input.ErrInvalid
	}
	allowed := map[string]bool{}
	for _, key := range splitAuthoredFields(required) {
		allowed[key] = true
		if _, ok := fields[key]; !ok {
			return nil, v2input.ErrInvalid
		}
	}
	for _, key := range optional {
		allowed[key] = true
	}
	for key := range fields {
		if !allowed[key] {
			return nil, v2input.ErrInvalid
		}
	}
	return fields, nil
}
func splitAuthoredFields(value string) []string {
	var result []string
	start := 0
	for i := 0; i <= len(value); i++ {
		if i == len(value) || value[i] == ' ' {
			if i > start {
				result = append(result, value[start:i])
			}
			start = i + 1
		}
	}
	return result
}
func authoredString(raw []byte) (string, bool) {
	var value string
	err := json.Unmarshal(raw, &value)
	return value, err == nil && !bytes.Equal(raw, []byte("null"))
}
func authoredUUID(raw []byte) (uuid.UUID, bool) {
	s, ok := authoredString(raw)
	if !ok {
		return uuid.Nil, false
	}
	v, e := uuid.Parse(s)
	return v, e == nil && v != uuid.Nil && v.String() == s
}
func authoredInt(raw []byte, min, max int64) (int64, bool) {
	v, e := strconv.ParseInt(string(raw), 10, 64)
	return v, e == nil && v >= min && v <= max
}
func authoredSHA(raw []byte) bool { s, ok := authoredString(raw); return ok && correctionDigest(s) }
func authoredBinding(raw []byte) (v2input.SessionVersionBinding, bool) {
	f, e := authoredObject(raw, "asset_version_id canvas_hash source")
	if e != nil {
		return v2input.SessionVersionBinding{}, false
	}
	id, ok := authoredUUID(f["asset_version_id"])
	ref, good := layerRef(f["source"])
	if !ok || !good || !authoredSHA(f["canvas_hash"]) {
		return v2input.SessionVersionBinding{}, false
	}
	hash, _ := authoredString(f["canvas_hash"])
	return v2input.SessionVersionBinding{AssetVersionID: id, CanvasHash: hash, Source: ref}, true
}
func authoredArray(raw []byte, min, max int) ([]json.RawMessage, bool) {
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) != nil || list == nil || len(list) < min || len(list) > max {
		return nil, false
	}
	return list, true
}
func authoredProfile(jcs, hash json.RawMessage) bool {
	s, ok := authoredString(jcs)
	if !ok || !authoredSHA(hash) {
		return false
	}
	wire, e := canonical.Bytes([]byte(s))
	if e != nil || string(wire) != s {
		return false
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(wire, &obj) != nil || len(obj) == 0 {
		return false
	}
	sum := sha256.Sum256(wire)
	expected, _ := authoredString(hash)
	return hex.EncodeToString(sum[:]) == expected
}

type authoredEdges struct {
	path     string
	parent   v2input.Ref
	children []LayerRef
	err      error
}

func (a *authoredEdges) add(path, contract string, raw json.RawMessage) {
	ref, ok := layerRef(raw)
	if !ok {
		a.err = v2input.ErrInvalid
		return
	}
	a.children = append(a.children, LayerRef{Path: a.path + path, Contract: contract, Ref: ref, ParentRef: a.parent})
}
func (a *authoredEdges) binding(path string, raw json.RawMessage) (v2input.SessionVersionBinding, bool) {
	b, ok := authoredBinding(raw)
	if !ok {
		a.err = v2input.ErrInvalid
		return b, false
	}
	a.children = append(a.children, LayerRef{Path: a.path + path + ".source", Contract: "WorkingImage", Ref: b.Source, CanvasHash: b.CanvasHash, ParentRef: a.parent})
	return b, true
}
func (a *authoredEdges) refs(path, contract string, raw json.RawMessage, min, max int, unique bool) []v2input.Ref {
	list, ok := authoredArray(raw, min, max)
	if !ok {
		a.err = v2input.ErrInvalid
		return nil
	}
	refs := []v2input.Ref{}
	seen := map[uuid.UUID]bool{}
	for i, item := range list {
		ref, valid := layerRef(item)
		if !valid || unique && seen[ref.ArtifactID] {
			a.err = v2input.ErrInvalid
			return nil
		}
		seen[ref.ArtifactID] = true
		refs = append(refs, ref)
		a.add(path+"["+strconv.Itoa(i)+"]", contract, item)
	}
	return refs
}
