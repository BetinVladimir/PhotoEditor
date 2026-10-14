package semanticproposal

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestNativeFaceResultDecode(t *testing.T) {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	params, source, faces, detectorParams := ref(), ref(), ref(), ref()
	paramsBody, _ := json.Marshal(params)
	forbidden := []v2input.Ref{source, faces, detectorParams}
	for _, operation := range []string{"face.landmarks", "face.parse"} {
		outputs := []v2input.Ref{ref()}
		if operation == "face.parse" {
			outputs = append(outputs, ref())
		}
		raw, _ := json.Marshal(outputs)
		id := uuid.New()
		got, err := decodeNativeFaceResultCatalog(id, raw, paramsBody, operation, params, forbidden)
		if err != nil || got.ResultID != id || len(got.Outputs) != len(outputs) || got.Params != params {
			t.Fatal(operation, err)
		}
		bad := [][]byte{nil, []byte("null"), []byte("{}"), []byte("[]"), append(append([]byte(nil), raw...), []byte(" []")...), []byte(strings.Replace(string(raw), "{", "{\"unknown\":1,", 1))}
		for _, foreign := range append(append([]v2input.Ref(nil), forbidden...), params) {
			changed := append([]v2input.Ref(nil), outputs...)
			changed[0] = foreign
			value, _ := json.Marshal(changed)
			bad = append(bad, value)
		}
		if len(outputs) == 2 {
			value, _ := json.Marshal([]v2input.Ref{outputs[0], outputs[0]})
			bad = append(bad, value)
		}
		for _, value := range bad {
			got, err := decodeNativeFaceResultCatalog(id, value, paramsBody, operation, params, forbidden)
			if err == nil || got.ResultID != uuid.Nil || got.Outputs != nil {
				t.Fatal("invalid face result exposed references")
			}
		}
		if _, err := decodeNativeFaceResultCatalog(uuid.Nil, raw, paramsBody, operation, params, forbidden); err == nil {
			t.Fatal("nil result accepted")
		}
		if _, err := decodeNativeFaceResultCatalog(id, raw, paramsBody, operation, ref(), forbidden); err == nil {
			t.Fatal("foreign Params accepted")
		}
	}
}
