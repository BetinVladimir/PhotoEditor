package v2render

import (
	"bytes"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

const authoredDetailPythonVector = "{\"original\":\"{\\\"image\\\":{\\\"artifact_id\\\":\\\"7670b4ae-f281-425e-a26b-30e5489346f6\\\",\\\"sha256\\\":\\\"a5b1ea195365bb29090212cdf2e0cbd8fc6dc025128f8282796b38eed28146b3\\\"},\\\"protected\\\":[{\\\"artifact\\\":{\\\"artifact_id\\\":\\\"03d29c12-b620-410f-b5bc-049a21ba4365\\\",\\\"sha256\\\":\\\"1111111111111111111111111111111111111111111111111111111111111111\\\"},\\\"canvas\\\":{\\\"height\\\":3,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"original\\\",\\\"width\\\":4},\\\"confidence\\\":1,\\\"model_lock_id\\\":null,\\\"object_id\\\":null,\\\"protected\\\":true,\\\"selector\\\":\\\"protected\\\",\\\"transform_chain\\\":{\\\"hash\\\":\\\"4430e7786edc0f8419f02e909c15422ebf572287a58132d8f6f33250ce053121\\\",\\\"steps\\\":[]}}],\\\"regions\\\":[{\\\"confirmed\\\":true,\\\"kind\\\":\\\"teeth\\\",\\\"mask\\\":{\\\"artifact\\\":{\\\"artifact_id\\\":\\\"831ce8eb-0779-451e-bb1d-d2e638c4ea61\\\",\\\"sha256\\\":\\\"1111111111111111111111111111111111111111111111111111111111111111\\\"},\\\"canvas\\\":{\\\"height\\\":3,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"original\\\",\\\"width\\\":4},\\\"confidence\\\":1,\\\"model_lock_id\\\":null,\\\"object_id\\\":null,\\\"protected\\\":false,\\\"selector\\\":\\\"manual\\\",\\\"transform_chain\\\":{\\\"hash\\\":\\\"4430e7786edc0f8419f02e909c15422ebf572287a58132d8f6f33250ce053121\\\",\\\"steps\\\":[]}},\\\"region_id\\\":\\\"teeth\\\",\\\"strength\\\":1}],\\\"resolved\\\":{\\\"artifact_id\\\":\\\"2e660e23-78ab-48c0-a973-f375a552bed5\\\",\\\"sha256\\\":\\\"76afe36d3e501c4d47ab6a1ef4b3729e8ca3b23d37ef1233aefc7a150163b00e\\\"}}\",\"current\":\"{\\\"alpha\\\":null,\\\"canvas\\\":{\\\"height\\\":6,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"resized\\\",\\\"width\\\":8},\\\"color\\\":{\\\"encoding\\\":\\\"gamma24\\\",\\\"icc_sha256\\\":\\\"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\\\",\\\"profile_id\\\":\\\"work_rec2020_gamma24_v1\\\"},\\\"dtype\\\":\\\"float32\\\",\\\"pixels\\\":{\\\"artifact_id\\\":\\\"cc490545-eca8-4e3f-9df2-20cd57521459\\\",\\\"sha256\\\":\\\"1111111111111111111111111111111111111111111111111111111111111111\\\"},\\\"transform_chain\\\":{\\\"hash\\\":\\\"1df2335afd170d979be8ea2d19eb72725c66ea2d81eefc77fc2978936d08c930\\\",\\\"steps\\\":[{\\\"from_canvas\\\":{\\\"height\\\":3,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"original\\\",\\\"width\\\":4},\\\"invertibility\\\":\\\"invertible\\\",\\\"kind\\\":\\\"resize\\\",\\\"pixel_centers\\\":\\\"half_pixel\\\",\\\"to_canvas\\\":{\\\"height\\\":6,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"resized\\\",\\\"width\\\":8},\\\"valid_region\\\":{\\\"height\\\":6,\\\"width\\\":8,\\\"x\\\":0,\\\"y\\\":0}}]}}\",\"current_ref\":{\"artifact_id\":\"87c474fe-34c0-434f-a786-f5f9991a98f3\",\"sha256\":\"909c9cc92b99f9ac012b567de0b41093b3125c6ba5c44c21833ef159b3bc6738\"},\"documents\":[{\"root\":\"{\\\"algorithm\\\":\\\"mask_transform_v1\\\",\\\"dtype\\\":\\\"float32\\\",\\\"guided_radius\\\":1,\\\"mask\\\":{\\\"artifact\\\":{\\\"artifact_id\\\":\\\"0b572f1d-51c7-4ece-945c-2f489841033f\\\",\\\"sha256\\\":\\\"1111111111111111111111111111111111111111111111111111111111111111\\\"},\\\"canvas\\\":{\\\"height\\\":6,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"resized\\\",\\\"width\\\":8},\\\"confidence\\\":1,\\\"model_lock_id\\\":null,\\\"object_id\\\":null,\\\"protected\\\":false,\\\"selector\\\":\\\"manual\\\",\\\"transform_chain\\\":{\\\"hash\\\":\\\"1df2335afd170d979be8ea2d19eb72725c66ea2d81eefc77fc2978936d08c930\\\",\\\"steps\\\":[{\\\"from_canvas\\\":{\\\"height\\\":3,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"original\\\",\\\"width\\\":4},\\\"invertibility\\\":\\\"invertible\\\",\\\"kind\\\":\\\"resize\\\",\\\"pixel_centers\\\":\\\"half_pixel\\\",\\\"to_canvas\\\":{\\\"height\\\":6,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"resized\\\",\\\"width\\\":8},\\\"valid_region\\\":{\\\"height\\\":6,\\\"width\\\":8,\\\"x\\\":0,\\\"y\\\":0}}]}},\\\"params\\\":{\\\"dtype\\\":\\\"float32\\\",\\\"mask\\\":{\\\"artifact\\\":{\\\"artifact_id\\\":\\\"831ce8eb-0779-451e-bb1d-d2e638c4ea61\\\",\\\"sha256\\\":\\\"1111111111111111111111111111111111111111111111111111111111111111\\\"},\\\"canvas\\\":{\\\"height\\\":3,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"original\\\",\\\"width\\\":4},\\\"confidence\\\":1,\\\"model_lock_id\\\":null,\\\"object_id\\\":null,\\\"protected\\\":false,\\\"selector\\\":\\\"manual\\\",\\\"transform_chain\\\":{\\\"hash\\\":\\\"4430e7786edc0f8419f02e909c15422ebf572287a58132d8f6f33250ce053121\\\",\\\"steps\\\":[]}},\\\"step\\\":{\\\"from_canvas\\\":{\\\"height\\\":3,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"original\\\",\\\"width\\\":4},\\\"invertibility\\\":\\\"invertible\\\",\\\"kind\\\":\\\"resize\\\",\\\"pixel_centers\\\":\\\"half_pixel\\\",\\\"to_canvas\\\":{\\\"height\\\":6,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"resized\\\",\\\"width\\\":8},\\\"valid_region\\\":{\\\"height\\\":6,\\\"width\\\":8,\\\"x\\\":0,\\\"y\\\":0}},\\\"target\\\":{\\\"artifact_id\\\":\\\"87c474fe-34c0-434f-a786-f5f9991a98f3\\\",\\\"sha256\\\":\\\"909c9cc92b99f9ac012b567de0b41093b3125c6ba5c44c21833ef159b3bc6738\\\"}},\\\"png\\\":{\\\"artifact_id\\\":\\\"781cf7e7-b254-4566-a7a0-c48c9125d5f2\\\",\\\"sha256\\\":\\\"1111111111111111111111111111111111111111111111111111111111111111\\\"}}\",\"target\":\"{\\\"alpha\\\":null,\\\"canvas\\\":{\\\"height\\\":6,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"resized\\\",\\\"width\\\":8},\\\"color\\\":{\\\"encoding\\\":\\\"gamma24\\\",\\\"icc_sha256\\\":\\\"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\\\",\\\"profile_id\\\":\\\"work_rec2020_gamma24_v1\\\"},\\\"dtype\\\":\\\"float32\\\",\\\"pixels\\\":{\\\"artifact_id\\\":\\\"cc490545-eca8-4e3f-9df2-20cd57521459\\\",\\\"sha256\\\":\\\"1111111111111111111111111111111111111111111111111111111111111111\\\"},\\\"transform_chain\\\":{\\\"hash\\\":\\\"1df2335afd170d979be8ea2d19eb72725c66ea2d81eefc77fc2978936d08c930\\\",\\\"steps\\\":[{\\\"from_canvas\\\":{\\\"height\\\":3,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"original\\\",\\\"width\\\":4},\\\"invertibility\\\":\\\"invertible\\\",\\\"kind\\\":\\\"resize\\\",\\\"pixel_centers\\\":\\\"half_pixel\\\",\\\"to_canvas\\\":{\\\"height\\\":6,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"resized\\\",\\\"width\\\":8},\\\"valid_region\\\":{\\\"height\\\":6,\\\"width\\\":8,\\\"x\\\":0,\\\"y\\\":0}}]}}\",\"target_ref\":{\"artifact_id\":\"87c474fe-34c0-434f-a786-f5f9991a98f3\",\"sha256\":\"909c9cc92b99f9ac012b567de0b41093b3125c6ba5c44c21833ef159b3bc6738\"}},{\"root\":\"{\\\"algorithm\\\":\\\"mask_transform_v1\\\",\\\"dtype\\\":\\\"float32\\\",\\\"guided_radius\\\":1,\\\"mask\\\":{\\\"artifact\\\":{\\\"artifact_id\\\":\\\"983d7503-40a4-4e61-9b76-6e9f4b623fcb\\\",\\\"sha256\\\":\\\"1111111111111111111111111111111111111111111111111111111111111111\\\"},\\\"canvas\\\":{\\\"height\\\":6,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"resized\\\",\\\"width\\\":8},\\\"confidence\\\":1,\\\"model_lock_id\\\":null,\\\"object_id\\\":null,\\\"protected\\\":true,\\\"selector\\\":\\\"protected\\\",\\\"transform_chain\\\":{\\\"hash\\\":\\\"1df2335afd170d979be8ea2d19eb72725c66ea2d81eefc77fc2978936d08c930\\\",\\\"steps\\\":[{\\\"from_canvas\\\":{\\\"height\\\":3,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"original\\\",\\\"width\\\":4},\\\"invertibility\\\":\\\"invertible\\\",\\\"kind\\\":\\\"resize\\\",\\\"pixel_centers\\\":\\\"half_pixel\\\",\\\"to_canvas\\\":{\\\"height\\\":6,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"resized\\\",\\\"width\\\":8},\\\"valid_region\\\":{\\\"height\\\":6,\\\"width\\\":8,\\\"x\\\":0,\\\"y\\\":0}}]}},\\\"params\\\":{\\\"dtype\\\":\\\"float32\\\",\\\"mask\\\":{\\\"artifact\\\":{\\\"artifact_id\\\":\\\"03d29c12-b620-410f-b5bc-049a21ba4365\\\",\\\"sha256\\\":\\\"1111111111111111111111111111111111111111111111111111111111111111\\\"},\\\"canvas\\\":{\\\"height\\\":3,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"original\\\",\\\"width\\\":4},\\\"confidence\\\":1,\\\"model_lock_id\\\":null,\\\"object_id\\\":null,\\\"protected\\\":true,\\\"selector\\\":\\\"protected\\\",\\\"transform_chain\\\":{\\\"hash\\\":\\\"4430e7786edc0f8419f02e909c15422ebf572287a58132d8f6f33250ce053121\\\",\\\"steps\\\":[]}},\\\"step\\\":{\\\"from_canvas\\\":{\\\"height\\\":3,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"original\\\",\\\"width\\\":4},\\\"invertibility\\\":\\\"invertible\\\",\\\"kind\\\":\\\"resize\\\",\\\"pixel_centers\\\":\\\"half_pixel\\\",\\\"to_canvas\\\":{\\\"height\\\":6,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"resized\\\",\\\"width\\\":8},\\\"valid_region\\\":{\\\"height\\\":6,\\\"width\\\":8,\\\"x\\\":0,\\\"y\\\":0}},\\\"target\\\":{\\\"artifact_id\\\":\\\"87c474fe-34c0-434f-a786-f5f9991a98f3\\\",\\\"sha256\\\":\\\"909c9cc92b99f9ac012b567de0b41093b3125c6ba5c44c21833ef159b3bc6738\\\"}},\\\"png\\\":{\\\"artifact_id\\\":\\\"4e33a8af-9ca6-40b9-82a7-ba223fb1646a\\\",\\\"sha256\\\":\\\"1111111111111111111111111111111111111111111111111111111111111111\\\"}}\",\"target\":\"{\\\"alpha\\\":null,\\\"canvas\\\":{\\\"height\\\":6,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"resized\\\",\\\"width\\\":8},\\\"color\\\":{\\\"encoding\\\":\\\"gamma24\\\",\\\"icc_sha256\\\":\\\"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\\\",\\\"profile_id\\\":\\\"work_rec2020_gamma24_v1\\\"},\\\"dtype\\\":\\\"float32\\\",\\\"pixels\\\":{\\\"artifact_id\\\":\\\"cc490545-eca8-4e3f-9df2-20cd57521459\\\",\\\"sha256\\\":\\\"1111111111111111111111111111111111111111111111111111111111111111\\\"},\\\"transform_chain\\\":{\\\"hash\\\":\\\"1df2335afd170d979be8ea2d19eb72725c66ea2d81eefc77fc2978936d08c930\\\",\\\"steps\\\":[{\\\"from_canvas\\\":{\\\"height\\\":3,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"original\\\",\\\"width\\\":4},\\\"invertibility\\\":\\\"invertible\\\",\\\"kind\\\":\\\"resize\\\",\\\"pixel_centers\\\":\\\"half_pixel\\\",\\\"to_canvas\\\":{\\\"height\\\":6,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"resized\\\",\\\"width\\\":8},\\\"valid_region\\\":{\\\"height\\\":6,\\\"width\\\":8,\\\"x\\\":0,\\\"y\\\":0}}]}}\",\"target_ref\":{\"artifact_id\":\"87c474fe-34c0-434f-a786-f5f9991a98f3\",\"sha256\":\"909c9cc92b99f9ac012b567de0b41093b3125c6ba5c44c21833ef159b3bc6738\"}}],\"expected\":\"{\\\"image\\\":{\\\"artifact_id\\\":\\\"87c474fe-34c0-434f-a786-f5f9991a98f3\\\",\\\"sha256\\\":\\\"909c9cc92b99f9ac012b567de0b41093b3125c6ba5c44c21833ef159b3bc6738\\\"},\\\"protected\\\":[{\\\"artifact\\\":{\\\"artifact_id\\\":\\\"983d7503-40a4-4e61-9b76-6e9f4b623fcb\\\",\\\"sha256\\\":\\\"1111111111111111111111111111111111111111111111111111111111111111\\\"},\\\"canvas\\\":{\\\"height\\\":6,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"resized\\\",\\\"width\\\":8},\\\"confidence\\\":1,\\\"model_lock_id\\\":null,\\\"object_id\\\":null,\\\"protected\\\":true,\\\"selector\\\":\\\"protected\\\",\\\"transform_chain\\\":{\\\"hash\\\":\\\"1df2335afd170d979be8ea2d19eb72725c66ea2d81eefc77fc2978936d08c930\\\",\\\"steps\\\":[{\\\"from_canvas\\\":{\\\"height\\\":3,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"original\\\",\\\"width\\\":4},\\\"invertibility\\\":\\\"invertible\\\",\\\"kind\\\":\\\"resize\\\",\\\"pixel_centers\\\":\\\"half_pixel\\\",\\\"to_canvas\\\":{\\\"height\\\":6,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"resized\\\",\\\"width\\\":8},\\\"valid_region\\\":{\\\"height\\\":6,\\\"width\\\":8,\\\"x\\\":0,\\\"y\\\":0}}]}}],\\\"regions\\\":[{\\\"confirmed\\\":true,\\\"kind\\\":\\\"teeth\\\",\\\"mask\\\":{\\\"artifact\\\":{\\\"artifact_id\\\":\\\"0b572f1d-51c7-4ece-945c-2f489841033f\\\",\\\"sha256\\\":\\\"1111111111111111111111111111111111111111111111111111111111111111\\\"},\\\"canvas\\\":{\\\"height\\\":6,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"resized\\\",\\\"width\\\":8},\\\"confidence\\\":1,\\\"model_lock_id\\\":null,\\\"object_id\\\":null,\\\"protected\\\":false,\\\"selector\\\":\\\"manual\\\",\\\"transform_chain\\\":{\\\"hash\\\":\\\"1df2335afd170d979be8ea2d19eb72725c66ea2d81eefc77fc2978936d08c930\\\",\\\"steps\\\":[{\\\"from_canvas\\\":{\\\"height\\\":3,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"original\\\",\\\"width\\\":4},\\\"invertibility\\\":\\\"invertible\\\",\\\"kind\\\":\\\"resize\\\",\\\"pixel_centers\\\":\\\"half_pixel\\\",\\\"to_canvas\\\":{\\\"height\\\":6,\\\"orientation\\\":1,\\\"source_sha256\\\":\\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\\",\\\"space_id\\\":\\\"resized\\\",\\\"width\\\":8},\\\"valid_region\\\":{\\\"height\\\":6,\\\"width\\\":8,\\\"x\\\":0,\\\"y\\\":0}}]}},\\\"region_id\\\":\\\"teeth\\\",\\\"strength\\\":1}],\\\"resolved\\\":{\\\"artifact_id\\\":\\\"2e660e23-78ab-48c0-a973-f375a552bed5\\\",\\\"sha256\\\":\\\"76afe36d3e501c4d47ab6a1ef4b3729e8ca3b23d37ef1233aefc7a150163b00e\\\"}}\"}"

type authoredDetailVector struct {
	Original   string      `json:"original"`
	Current    string      `json:"current"`
	CurrentRef v2input.Ref `json:"current_ref"`
	Documents  []struct {
		Root      string      `json:"root"`
		Target    string      `json:"target"`
		TargetRef v2input.Ref `json:"target_ref"`
	} `json:"documents"`
	Expected string `json:"expected"`
}

func detailMaterializationFixture(t *testing.T) (authoredDetailVector, []AuthoredDetailTransformDocument) {
	t.Helper()
	var v authoredDetailVector
	if json.Unmarshal([]byte(authoredDetailPythonVector), &v) != nil {
		t.Fatal("vector malformed")
	}
	docs := make([]AuthoredDetailTransformDocument, len(v.Documents))
	for i, d := range v.Documents {
		docs[i] = AuthoredDetailTransformDocument{Root: []byte(d.Root), Target: []byte(d.Target), TargetRef: d.TargetRef}
	}
	return v, docs
}
func TestAuthoredDetailMaterializationMatchesActualPython(t *testing.T) {
	v, docs := detailMaterializationFixture(t)
	got, err := CompileAuthoredDetailOccurrence([]byte(v.Original), []byte(v.Current), v.CurrentRef, docs)
	if err != nil || !bytes.Equal(got, []byte(v.Expected)) {
		t.Fatalf("actual Python materialization mismatch: %v\n%s\n%s", err, got, v.Expected)
	}
	if string(docs[0].Root) != v.Documents[0].Root {
		t.Fatal("input mutated")
	}
}
func TestAuthoredDetailMaterializationRejectsMissingAndForeignTransforms(t *testing.T) {
	for _, change := range []string{"missing", "duplicate", "provenance", "target", "chain"} {
		t.Run(change, func(t *testing.T) {
			v, docs := detailMaterializationFixture(t)
			switch change {
			case "missing":
				docs = docs[:1]
			case "duplicate":
				docs = append(docs, docs[0])
			case "target":
				docs[0].TargetRef = v2input.Ref{}
			case "provenance", "chain":
				var root map[string]json.RawMessage
				json.Unmarshal(docs[0].Root, &root)
				var mask map[string]json.RawMessage
				json.Unmarshal(root["mask"], &mask)
				if change == "provenance" {
					mask["confidence"] = json.RawMessage(`0.2`)
				} else {
					mask["transform_chain"] = json.RawMessage(`{"hash":"bad","steps":[]}`)
				}
				root["mask"], _ = canonical.Marshal(mask)
				docs[0].Root, _ = canonical.Marshal(root)
			}
			if _, err := CompileAuthoredDetailOccurrence([]byte(v.Original), []byte(v.Current), v.CurrentRef, docs); err == nil {
				t.Fatal("unbound transform accepted")
			}
		})
	}
}

// Shared region/DSL occurrences consume one exact canonical producer path;
// unrelated valid paths stay visible to the owning publication's union check.
func TestAuthoredMaskPathsPartitionRepeatedOccurrences(t *testing.T) {
	v, docs := detailMaterializationFixture(t)
	var original struct {
		Regions []struct {
			Mask json.RawMessage `json:"mask"`
		} `json:"regions"`
	}
	if err := json.Unmarshal([]byte(v.Original), &original); err != nil {
		t.Fatal(err)
	}
	masks := []json.RawMessage{original.Regions[0].Mask, original.Regions[0].Mask}
	out, used, err := compileAuthoredMaskPaths(masks, []byte(v.Current), v.CurrentRef, docs)
	if err != nil || len(out) != 2 || !bytes.Equal(out[0], out[1]) || len(used) != 1 || used[0] != 0 {
		t.Fatalf("repeated exact occurrence: used=%v err=%v", used, err)
	}
	if _, err := CompileAuthoredDetailOccurrence([]byte(v.Original), []byte(v.Current), v.CurrentRef, append(docs, docs[0])); err == nil {
		t.Fatal("ambiguous producer path accepted")
	}
}
func TestAuthoredMaskPathsNeverTreatDifferentTargetAsIdentity(t *testing.T) {
	v, docs := detailMaterializationFixture(t)
	var original struct {
		Regions []struct {
			Mask json.RawMessage `json:"mask"`
		} `json:"regions"`
	}
	if err := json.Unmarshal([]byte(v.Original), &original); err != nil {
		t.Fatal(err)
	}
	if _, _, err := compileAuthoredMaskPaths([]json.RawMessage{original.Regions[0].Mask}, []byte(v.Current), v.CurrentRef, nil); err == nil {
		t.Fatal("missing actual transport accepted")
	}
	if _, _, err := compileAuthoredMaskPaths([]json.RawMessage{original.Regions[0].Mask}, []byte(v.Current), v2input.Ref{}, docs); err == nil {
		t.Fatal("unbound current target accepted")
	}
}
