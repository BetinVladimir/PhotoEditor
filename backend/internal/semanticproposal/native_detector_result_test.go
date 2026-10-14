package semanticproposal

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestNativeDetectorResultCatalogDecode(t *testing.T) {
	source := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	params := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	output := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	id := uuid.New()
	paramsBody, _ := json.Marshal(params)
	outputBody, _ := json.Marshal(output)
	result, err := decodeNativeDetectorResultCatalog(id, outputBody, paramsBody, source, params)
	if err != nil || result.ResultID != id || result.Output != output || result.Params != params {
		t.Fatal("exact catalog", err)
	}
	for _, bad := range [][]byte{nil, []byte("  "), []byte("null"), []byte("[]"), append(append([]byte(nil), outputBody...), []byte(" {}")...), []byte(strings.Replace(string(outputBody), "{", "{\"extra\":1,", 1)), paramsBody, []byte(strings.Replace(string(outputBody), output.SHA256, "broken", 1))} {
		got, err := decodeNativeDetectorResultCatalog(id, bad, paramsBody, source, params)
		if err == nil || got != (nativeDetectorResultCatalog{}) {
			t.Fatal("invalid catalog exposed output")
		}
	}
	changed := params
	changed.ArtifactID = uuid.New()
	if _, err := decodeNativeDetectorResultCatalog(id, outputBody, paramsBody, source, changed); err == nil {
		t.Fatal("substituted frozen Params")
	}
	if _, err := decodeNativeDetectorResultCatalog(uuid.Nil, outputBody, paramsBody, source, params); err == nil {
		t.Fatal("nil canonical result")
	}
	sourceBody, _ := json.Marshal(source)
	if _, err := decodeNativeDetectorResultCatalog(id, sourceBody, paramsBody, source, params); err == nil {
		t.Fatal("output aliases source")
	}
}
