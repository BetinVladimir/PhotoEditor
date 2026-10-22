// Public fixture construction only: real compiler output, no database or qualification.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/colorprofile"
	"photoeditor/backend/internal/media"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/workflow"
)

func run() error {
	dir := "/Users/freelancer/Documents/PhotoEditor/work/rtx-media-full-adapter-probe-r1"
	raw, e := os.ReadFile(filepath.Join(dir, "public-working-image.json"))
	if e != nil {
		return e
	}
	var working review.WorkingImage
	if e = json.Unmarshal(raw, &working); e != nil {
		return e
	}
	bindingRaw, e := os.ReadFile(filepath.Join(dir, "public-binding.json"))
	if e != nil {
		return e
	}
	var binding struct {
		Master review.ArtifactRef `json:"master"`
	}
	if e = json.Unmarshal(bindingRaw, &binding); e != nil {
		return e
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != binding.Master.SHA256 {
		return fmt.Errorf("public master fixture digest mismatch")
	}
	profiles, e := colorprofile.LoadSHAs("/Users/freelancer/Documents/PhotoEditor/resources/icc")
	if e != nil {
		return e
	}
	master := workflow.MediaMaster{Artifact: binding.Master, Bytes: int64(len(raw)), Width: int64(working.Canvas.Width), Height: int64(working.Canvas.Height), MediaType: "application/json", Working: &working}
	event := uuid.NewSHA1(uuid.NameSpaceURL, []byte("framevela-diagnostic-public-media-event-r1"))
	version := uuid.NewSHA1(event, []byte("version"))
	documents, e := workflow.BuildMediaRetryDocumentsForPolicy(event, version, master, []string{"thumbnail", "preview"}, profiles["srgb_v1"], media.CurrentRendererPolicy)
	if e != nil {
		return e
	}
	type fixture struct {
		Command          map[string]any       `json:"command"`
		Variant          string               `json:"variant"`
		Operation        string               `json:"operation"`
		Params           json.RawMessage      `json:"params"`
		ParamsRef        review.ArtifactRef   `json:"params_ref"`
		Inputs           []review.ArtifactRef `json:"inputs"`
		ExecutionProfile json.RawMessage      `json:"execution_profile"`
		InputDigest      string               `json:"input_digest"`
	}
	fixtures := []fixture{}
	for _, d := range documents {
		h := sha256.Sum256(d.Params)
		ref := review.ArtifactRef{ID: uuid.NewSHA1(d.ID, []byte("diagnostic-params")), SHA256: hex.EncodeToString(h[:])}
		task, e := workflow.PrepareMediaRetryTask(workflow.PublishedMediaRetry{Document: d, Params: ref}, master)
		if e != nil {
			return e
		}
		command := map[string]any{"schema_version": 1, "event_id": uuid.NewSHA1(d.ID, []byte("event")), "tenant_id": uuid.NewSHA1(event, []byte("diagnostic-tenant")), "run_id": uuid.NewSHA1(event, []byte("diagnostic-run")), "task_id": task.ID, "attempt": 1, "operation": task.Operation, "operation_version": task.Version, "resource_class": task.ResourceClass, "correlation_id": uuid.NewSHA1(d.ID, []byte("correlation")), "inputs": task.Inputs, "params": ref, "model": nil, "timeout_seconds": task.TimeoutSeconds, "traceparent": ""}
		fixtures = append(fixtures, fixture{command, d.Variant, d.Operation, d.Params, ref, task.Inputs, task.ExecutionProfile, task.InputDigest})
	}
	output := map[string]any{"status": "ACTUAL_GO_MEDIA_COMPILER_PUBLIC_FIXTURES_NOT_QUALIFICATION", "event_id": event, "version_id": version, "renderer_policy": media.CurrentRendererPolicy, "srgb_sha256": profiles["srgb_v1"], "master": binding.Master, "fixtures": fixtures, "rows": fixtures, "diagnostic_ids_only_no_db_authority": true}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintf(os.Stderr, "PUBLIC_FIXTURE_FAILURE_CLASS=%T\n", e)
		os.Exit(1)
	}
}
