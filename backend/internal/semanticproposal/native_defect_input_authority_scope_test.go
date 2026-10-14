package semanticproposal

import (
	"context"
	"errors"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestNativeDefectUnsupportedAuthorityRejectedBeforePublication(t *testing.T) {
	for _, tc := range []struct {
		params, roots string
		additional    []nativeDefectInputPin
		valid         bool
	}{
		{`{"permanent":null,"protections":[]}`, `[]`, nil, true},
		{`{"permanent":{}}`, `[]`, nil, false},
		{`{"protections":[{}]}`, `[]`, nil, false},
		{`{}`, `[{}]`, nil, false},
		{`{}`, `[]`, []nativeDefectInputPin{{}}, false},
		{`invalid`, `[]`, nil, false},
	} {
		err := validateNativeDefectInputAuthorityScope(context.Background(), PreparedDefectParams{params: tc.params, roots: tc.roots}, tc.additional)
		if (err == nil) != tc.valid {
			t.Fatal("authority scope", err)
		}
		if !tc.valid && !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := validateNativeDefectInputAuthorityScope(ctx, PreparedDefectParams{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
