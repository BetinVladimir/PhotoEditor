package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestAuthoredDetailPublishedParamsBindingPreservesLayer(t *testing.T) {
	old := v2input.Ref{ArtifactID: uuid.New(), SHA256: string(bytes.Repeat([]byte("a"), 64))}
	next := v2input.Ref{ArtifactID: uuid.New(), SHA256: string(bytes.Repeat([]byte("b"), 64))}
	layer := map[string]any{"id": uuid.New().String(), "operation": "portrait.detail", "operation_version": "1", "enabled": true, "opacity": 0.37, "blend": "normal", "params": old, "mask": map[string]any{"op": "invert", "operand": map[string]any{"op": "constant", "value": 0.25}}}
	raw, err := canonical.Marshal(map[string]any{"schema_version": 1, "source": map[string]any{"retained": "source"}, "layers": []any{layer}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := bindAuthoredDetailParams(raw, next)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]json.RawMessage
	var layers []map[string]json.RawMessage
	if json.Unmarshal(got, &body) != nil || json.Unmarshal(body["layers"], &layers) != nil {
		t.Fatal("invalid output")
	}
	var bound v2input.Ref
	if json.Unmarshal(layers[0]["params"], &bound) != nil || bound != next {
		t.Fatal("actual published Params not bound")
	}
	original, _ := canonical.Marshal(old)
	layers[0]["params"] = original
	body["layers"], _ = canonical.Marshal(layers)
	restored, _ := canonical.Marshal(body)
	if !bytes.Equal(restored, raw) {
		t.Fatal("published binding changed original layer or source")
	}
	var actual v2input.Ref
	if json.Unmarshal(layers[0]["params"], &actual) != nil || actual != old {
		t.Fatal("test inverse failed")
	}
}

func TestAuthoredDetailContinuationRefusesBeforePublication(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (SQLAuthoredDetailContinuation{}).Resume(ctx, auth.Identity{}, uuid.Nil, uuid.Nil, 1, nil, nil, nil, nil, nil, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := (SQLAuthoredDetailContinuation{}).Resume(context.Background(), auth.Identity{}, uuid.Nil, uuid.Nil, 1, nil, nil, nil, nil, nil, nil, nil); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{nil, []byte(`{}`), []byte(`{"layers":[]}`), []byte(`{"layers":[{"operation":"image.denoise","operation_version":"1"}]}`)} {
		if _, err := bindAuthoredDetailParams(raw, v2input.Ref{ArtifactID: uuid.New(), SHA256: string(bytes.Repeat([]byte("a"), 64))}); err == nil {
			t.Fatal("malformed or foreign layer accepted")
		}
	}
}

const authoredDetailBindingPythonVector = "{\n  \"source\": {\n    \"tenant\": \"00000000-0000-0000-0000-0000000003e9\",\n    \"parent\": \"00000000-0000-0000-0000-0000000003ea\",\n    \"instruction\": {\n      \"kind\": \"segment\",\n      \"layers\": [\n        \"1a7d690c-dc85-4164-82e4-804a1dd860a5\"\n      ]\n    },\n    \"source\": {\n      \"asset_version_id\": \"97f78d3f-fd2d-4849-af78-eba068088c66\",\n      \"source\": {\n        \"artifact_id\": \"5634321b-8631-4f38-a674-a5abb72c2f05\",\n        \"sha256\": \"73fe2658f4cf461f0733338dfc7f84baba8793ea45e83d7147dd7cbe37ba2605\"\n      },\n      \"canvas_hash\": \"0d6caa571b5edec6928c0ed2cde296edd13bf0759f031046b0fea2f92b20be26\"\n    },\n    \"layer_stack_jcs\": \"{\\\"layers\\\":[{\\\"blend\\\":\\\"multiply\\\",\\\"enabled\\\":true,\\\"id\\\":\\\"1a7d690c-dc85-4164-82e4-804a1dd860a5\\\",\\\"mask\\\":{\\\"left\\\":{\\\"left\\\":{\\\"canvas_hash\\\":\\\"d924f797418963ffbea9d5529d651870549fefdfc31f905a90ab2de690274ff0\\\",\\\"mask\\\":{\\\"artifact_id\\\":\\\"77226665-8403-4376-afe6-7fb107b105cd\\\",\\\"sha256\\\":\\\"27aaa1c4f61f7d03e1e3b3679f04fcec09207304a6934b4fd00eef3d3d45b431\\\"},\\\"op\\\":\\\"ref\\\"},\\\"op\\\":\\\"union\\\",\\\"right\\\":{\\\"op\\\":\\\"invert\\\",\\\"operand\\\":{\\\"canvas_hash\\\":\\\"d924f797418963ffbea9d5529d651870549fefdfc31f905a90ab2de690274ff0\\\",\\\"mask\\\":{\\\"artifact_id\\\":\\\"77226665-8403-4376-afe6-7fb107b105cd\\\",\\\"sha256\\\":\\\"27aaa1c4f61f7d03e1e3b3679f04fcec09207304a6934b4fd00eef3d3d45b431\\\"},\\\"op\\\":\\\"ref\\\"}}},\\\"op\\\":\\\"subtract\\\",\\\"right\\\":{\\\"left\\\":{\\\"canvas_hash\\\":\\\"d924f797418963ffbea9d5529d651870549fefdfc31f905a90ab2de690274ff0\\\",\\\"mask\\\":{\\\"artifact_id\\\":\\\"77226665-8403-4376-afe6-7fb107b105cd\\\",\\\"sha256\\\":\\\"27aaa1c4f61f7d03e1e3b3679f04fcec09207304a6934b4fd00eef3d3d45b431\\\"},\\\"op\\\":\\\"ref\\\"},\\\"op\\\":\\\"intersect\\\",\\\"right\\\":{\\\"canvas_hash\\\":\\\"d924f797418963ffbea9d5529d651870549fefdfc31f905a90ab2de690274ff0\\\",\\\"mask\\\":{\\\"artifact_id\\\":\\\"77226665-8403-4376-afe6-7fb107b105cd\\\",\\\"sha256\\\":\\\"27aaa1c4f61f7d03e1e3b3679f04fcec09207304a6934b4fd00eef3d3d45b431\\\"},\\\"op\\\":\\\"ref\\\"}}},\\\"opacity\\\":0.35,\\\"operation\\\":\\\"portrait.detail\\\",\\\"operation_version\\\":\\\"1\\\",\\\"params\\\":{\\\"artifact_id\\\":\\\"77226665-8403-4376-afe6-7fb107b105cd\\\",\\\"sha256\\\":\\\"27aaa1c4f61f7d03e1e3b3679f04fcec09207304a6934b4fd00eef3d3d45b431\\\"}}],\\\"schema_version\\\":1,\\\"source\\\":{\\\"asset_version_id\\\":\\\"97f78d3f-fd2d-4849-af78-eba068088c66\\\",\\\"canvas_hash\\\":\\\"d924f797418963ffbea9d5529d651870549fefdfc31f905a90ab2de690274ff0\\\",\\\"source\\\":{\\\"artifact_id\\\":\\\"77226665-8403-4376-afe6-7fb107b105cd\\\",\\\"sha256\\\":\\\"27aaa1c4f61f7d03e1e3b3679f04fcec09207304a6934b4fd00eef3d3d45b431\\\"}}}\"\n  },\n  \"params\": {\n    \"artifact_id\": \"ae865351-1778-4641-8aea-3099d2faf8b5\",\n    \"sha256\": \"d21062b11f0055840cd70e8e5e6b5ace2f5c1e4d926208086757d21d1cd57036\"\n  },\n  \"bindings\": [\n    {\n      \"source\": {\n        \"artifact_id\": \"77226665-8403-4376-afe6-7fb107b105cd\",\n        \"sha256\": \"27aaa1c4f61f7d03e1e3b3679f04fcec09207304a6934b4fd00eef3d3d45b431\"\n      },\n      \"current\": {\n        \"artifact_id\": \"ae865351-1778-4641-8aea-3099d2faf8b5\",\n        \"sha256\": \"d21062b11f0055840cd70e8e5e6b5ace2f5c1e4d926208086757d21d1cd57036\"\n      }\n    }\n  ],\n  \"expected\": \"{\\\"layers\\\":[{\\\"blend\\\":\\\"multiply\\\",\\\"enabled\\\":true,\\\"id\\\":\\\"1a7d690c-dc85-4164-82e4-804a1dd860a5\\\",\\\"mask\\\":{\\\"left\\\":{\\\"left\\\":{\\\"canvas_hash\\\":\\\"0d6caa571b5edec6928c0ed2cde296edd13bf0759f031046b0fea2f92b20be26\\\",\\\"mask\\\":{\\\"artifact_id\\\":\\\"ae865351-1778-4641-8aea-3099d2faf8b5\\\",\\\"sha256\\\":\\\"d21062b11f0055840cd70e8e5e6b5ace2f5c1e4d926208086757d21d1cd57036\\\"},\\\"op\\\":\\\"ref\\\"},\\\"op\\\":\\\"union\\\",\\\"right\\\":{\\\"op\\\":\\\"invert\\\",\\\"operand\\\":{\\\"canvas_hash\\\":\\\"0d6caa571b5edec6928c0ed2cde296edd13bf0759f031046b0fea2f92b20be26\\\",\\\"mask\\\":{\\\"artifact_id\\\":\\\"ae865351-1778-4641-8aea-3099d2faf8b5\\\",\\\"sha256\\\":\\\"d21062b11f0055840cd70e8e5e6b5ace2f5c1e4d926208086757d21d1cd57036\\\"},\\\"op\\\":\\\"ref\\\"}}},\\\"op\\\":\\\"subtract\\\",\\\"right\\\":{\\\"left\\\":{\\\"canvas_hash\\\":\\\"0d6caa571b5edec6928c0ed2cde296edd13bf0759f031046b0fea2f92b20be26\\\",\\\"mask\\\":{\\\"artifact_id\\\":\\\"ae865351-1778-4641-8aea-3099d2faf8b5\\\",\\\"sha256\\\":\\\"d21062b11f0055840cd70e8e5e6b5ace2f5c1e4d926208086757d21d1cd57036\\\"},\\\"op\\\":\\\"ref\\\"},\\\"op\\\":\\\"intersect\\\",\\\"right\\\":{\\\"canvas_hash\\\":\\\"0d6caa571b5edec6928c0ed2cde296edd13bf0759f031046b0fea2f92b20be26\\\",\\\"mask\\\":{\\\"artifact_id\\\":\\\"ae865351-1778-4641-8aea-3099d2faf8b5\\\",\\\"sha256\\\":\\\"d21062b11f0055840cd70e8e5e6b5ace2f5c1e4d926208086757d21d1cd57036\\\"},\\\"op\\\":\\\"ref\\\"}}},\\\"opacity\\\":0.35,\\\"operation\\\":\\\"portrait.detail\\\",\\\"operation_version\\\":\\\"1\\\",\\\"params\\\":{\\\"artifact_id\\\":\\\"ae865351-1778-4641-8aea-3099d2faf8b5\\\",\\\"sha256\\\":\\\"d21062b11f0055840cd70e8e5e6b5ace2f5c1e4d926208086757d21d1cd57036\\\"}}],\\\"schema_version\\\":1,\\\"source\\\":{\\\"asset_version_id\\\":\\\"97f78d3f-fd2d-4849-af78-eba068088c66\\\",\\\"canvas_hash\\\":\\\"0d6caa571b5edec6928c0ed2cde296edd13bf0759f031046b0fea2f92b20be26\\\",\\\"source\\\":{\\\"artifact_id\\\":\\\"5634321b-8631-4f38-a674-a5abb72c2f05\\\",\\\"sha256\\\":\\\"73fe2658f4cf461f0733338dfc7f84baba8793ea45e83d7147dd7cbe37ba2605\\\"}}}\"\n}"

func TestAuthoredDetailPublishedBindingUsesActualPythonStringVersion(t *testing.T) {
	var v struct {
		Params   v2input.Ref `json:"params"`
		Expected string      `json:"expected"`
	}
	if json.Unmarshal([]byte(authoredDetailBindingPythonVector), &v) != nil {
		t.Fatal("actual Python vector invalid")
	}
	got, err := bindAuthoredDetailParams([]byte(v.Expected), v.Params)
	if err != nil || !bytes.Equal(got, []byte(v.Expected)) {
		t.Fatalf("actual Python LayerStack refused: %v", err)
	}
	numeric := bytes.Replace([]byte(v.Expected), []byte(`"operation_version":"1"`), []byte(`"operation_version":1`), 1)
	if bytes.Equal(numeric, []byte(v.Expected)) {
		t.Fatal("actual Python string version missing")
	}
	if _, err := bindAuthoredDetailParams(numeric, v.Params); err == nil {
		t.Fatal("numeric version accepted")
	}
}
