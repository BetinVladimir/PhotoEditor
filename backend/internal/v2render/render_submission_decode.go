package v2render

import (
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// This bound accommodates the complete 5000-frame public selection.
const MaxRenderSubmissionBytes = 4 * 1024 * 1024

// DecodeRenderBatchSubmissionRequest rejects aliases, absent/null fields,
// additional keys and duplicates before decoding the immutable public intent.
func DecodeRenderBatchSubmissionRequest(raw []byte) (RenderBatchSubmissionRequest, error) {
	var out RenderBatchSubmissionRequest
	if len(raw) == 0 || len(raw) > MaxRenderSubmissionBytes {
		return out, v2input.ErrInvalid
	}
	body, err := canonical.Bytes(raw)
	if err != nil {
		return out, v2input.ErrInvalid
	}
	root, err := renderSubmissionFields(body, "input_snapshot_id", "policy_sha", "selection")
	if err != nil {
		return out, err
	}
	var items []json.RawMessage
	if json.Unmarshal(root["selection"], &items) != nil || len(items) < 1 || len(items) > 5000 {
		return out, v2input.ErrInvalid
	}
	for _, rawItem := range items {
		item, err := renderSubmissionFields(rawItem, "source", "recipe")
		if err != nil {
			return out, err
		}
		source, err := renderSubmissionFields(item["source"], "asset_version_id", "source", "canvas_hash")
		if err != nil {
			return out, err
		}
		if _, err := renderSubmissionFields(source["source"], "artifact_id", "sha256"); err != nil {
			return out, err
		}
		if _, err := renderSubmissionFields(item["recipe"], "artifact_id", "sha256"); err != nil {
			return out, err
		}
	}
	if json.Unmarshal(body, &out) != nil || validateRenderSubmissionRequest(out) != nil {
		return RenderBatchSubmissionRequest{}, v2input.ErrInvalid
	}
	return out, nil
}

func renderSubmissionFields(raw []byte, keys ...string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != len(keys) {
		return nil, v2input.ErrInvalid
	}
	for _, key := range keys {
		if len(fields[key]) == 0 || string(fields[key]) == "null" {
			return nil, v2input.ErrInvalid
		}
	}
	return fields, nil
}
