package v2draft

import (
	"photoeditor/backend/internal/v2input"
	"strconv"
)

func authoredNativeReplayRefs(raw []byte, contract, path string, parent v2input.Ref) ([]LayerRef, error) {
	var edges v2input.AuthoredReplayChildren
	var err error
	if contract == "AuthoredNativeReplayRequest" {
		edges, err = v2input.AuthoredNativeReplayRequestChildren(raw)
	} else if contract == "AuthoredNativeReplayEvidence" {
		edges, err = v2input.AuthoredNativeReplayEvidenceChildren(raw)
	} else {
		return nil, v2input.ErrInvalid
	}
	if err != nil {
		return nil, err
	}
	result := make([]LayerRef, 0, len(edges.Typed)+len(edges.Opaque))
	for i, c := range edges.Typed {
		label := c.Contract
		// ClosureResolver maps these exact binary contracts to canonical NpyArray
		// entries. They never pass through generic document/JSON interpretation.
		if label == "NpyArray" {
			label = "WorkingPixels"
		}
		result = append(result, LayerRef{Path: path + ".typed[" + strconv.Itoa(i) + "]", ParentRef: parent, Ref: c.Ref, Contract: label})
	}
	for i, ref := range edges.Opaque {
		result = append(result, LayerRef{Path: path + ".outputs[" + strconv.Itoa(i) + "]", ParentRef: parent, Ref: ref, Contract: "Artifact"})
	}
	return result, nil
}
