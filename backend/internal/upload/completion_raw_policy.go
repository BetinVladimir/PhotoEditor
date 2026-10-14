package upload

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"photoeditor/backend/internal/rawdevelopment"
)

func decodeCompletionRAWPolicy(raw []byte) (*rawdevelopment.Policy, error) {
	if raw == nil {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var policy rawdevelopment.Policy
	if decoder.Decode(&policy) != nil || !rawdevelopment.Valid(&policy) {
		return nil, errors.New("invalid persisted RAW policy")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return nil, errors.New("invalid persisted RAW policy")
	}
	return &policy, nil
}
