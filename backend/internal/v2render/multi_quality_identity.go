package v2render

import (
	"github.com/google/uuid"
	"strconv"
)

// Separate immutable quality run for exact multi producer/result/attempt/fence.
// This identity grants no source access and does not assert quality acceptance.
func MultiQualityRunID(source MultiPreviewSource) (uuid.UUID, error) {
	if err := validateMultiPreviewSource(source); err != nil {
		return uuid.Nil, err
	}
	name := "V29.MultiQuality:" + source.Binding.Operation + ":" + source.Producer.String() + ":" + source.Binding.ResultID.String() + ":" + source.Binding.Result.SHA256 + ":" + strconv.Itoa(source.Attempt) + ":" + strconv.FormatInt(source.Fence, 10)
	return uuid.NewSHA1(source.Binding.Run, []byte(name)), nil
}
