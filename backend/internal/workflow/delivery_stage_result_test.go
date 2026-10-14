package workflow

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestDeliveryStageResultRejectsForeignOrIncompleteProducerMetadata(t *testing.T) {
	task := uuid.New()
	for _, scenario := range []string{"valid", "foreign_task", "missing_report", "duplicate_id", "unknown_role", "foreign_bucket", "unversioned", "bad_sha", "bad_media", "zero_fence"} {
		t.Run(scenario, func(t *testing.T) {
			result := DeliveryStageResult{TaskID: task, ResultID: uuid.New(), Attempt: 1, Fence: 9}
			for _, role := range []string{"working_image", "derivative_report", "working_rgb"} {
				media := "application/json"
				if role == "working_rgb" {
					media = "application/octet-stream"
				}
				result.Outputs = append(result.Outputs, DeliveryStageOutput{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Role: role, Bucket: "photos", Key: "pinned/object", Version: "v1", Bytes: 100, MediaType: media})
			}
			switch scenario {
			case "foreign_task":
				result.TaskID = uuid.New()
			case "missing_report":
				result.Outputs = result.Outputs[:1]
			case "duplicate_id":
				result.Outputs[1].Ref.ArtifactID = result.Outputs[0].Ref.ArtifactID
			case "unknown_role":
				result.Outputs[1].Role = "other"
			case "foreign_bucket":
				result.Outputs[0].Bucket = "other"
			case "unversioned":
				result.Outputs[0].Version = "null"
			case "bad_sha":
				result.Outputs[0].Ref.SHA256 = strings.Repeat("z", 64)
			case "bad_media":
				result.Outputs[0].MediaType = "image/jpeg"
			case "zero_fence":
				result.Fence = 0
			}
			raw, _ := json.Marshal(result)
			parsed, err := parseDeliveryStageResult(raw, task, "photos")
			if scenario == "valid" {
				if err != nil || parsed.ResultID != result.ResultID {
					t.Fatal(parsed, err)
				}
			} else if err == nil {
				t.Fatal("invalid metadata accepted", scenario)
			}
		})
	}
}
