package interchange

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func closedInterchangeObject(raw []byte) (map[string]json.RawMessage, bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, false
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, false
		}
		name, ok := key.(string)
		if !ok {
			return nil, false
		}
		if _, exists := fields[name]; exists {
			return nil, false
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return nil, false
		}
		fields[name] = value
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') {
		return nil, false
	}
	_, err = decoder.Token()
	return fields, err == io.EOF
}

// The SQL function owns authorization. This boundary refuses malformed or
// cross-scope public projections before they reach an HTTP client.
func validInterchangeResource(raw []byte, tenantID, workflowID uuid.UUID) bool {
	if len(raw) < 2 || len(raw) > 1<<20 || tenantID == uuid.Nil || workflowID == uuid.Nil {
		return false
	}
	root, ok := closedInterchangeObject(raw)
	if !ok || len(root) != 4 {
		return false
	}
	for _, key := range []string{"id", "revision", "document", "data"} {
		if _, exists := root[key]; !exists {
			return false
		}
	}
	if string(root["document"]) != "null" {
		document, ok := closedInterchangeObject(root["document"])
		if !ok || len(document) != 2 {
			return false
		}
		for _, key := range []string{"artifact_id", "sha256"} {
			if _, exists := document[key]; !exists {
				return false
			}
		}
	}
	dataFields, ok := closedInterchangeObject(root["data"])
	if !ok || len(dataFields) != 13 {
		return false
	}
	for _, key := range []string{"resource_type", "id", "tenant_id", "project_id", "created_at", "updated_at", "revision", "workflow_id", "direction", "format", "mapping_artifact_id", "report_artifact_id", "bundle_artifact_id"} {
		if _, exists := dataFields[key]; !exists {
			return false
		}
	}
	var resource struct {
		ID       uuid.UUID    `json:"id"`
		Revision string       `json:"revision"`
		Document *v2input.Ref `json:"document"`
		Data     struct {
			Type      string     `json:"resource_type"`
			ID        uuid.UUID  `json:"id"`
			Tenant    uuid.UUID  `json:"tenant_id"`
			Project   uuid.UUID  `json:"project_id"`
			Created   time.Time  `json:"created_at"`
			Updated   time.Time  `json:"updated_at"`
			Revision  string     `json:"revision"`
			Workflow  uuid.UUID  `json:"workflow_id"`
			Direction string     `json:"direction"`
			Format    string     `json:"format"`
			Mapping   *uuid.UUID `json:"mapping_artifact_id"`
			Report    *uuid.UUID `json:"report_artifact_id"`
			Bundle    *uuid.UUID `json:"bundle_artifact_id"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&resource) != nil || decoder.Decode(new(any)) != io.EOF {
		return false
	}
	revision, err := strconv.ParseInt(resource.Revision, 10, 64)
	if err != nil || revision < 1 || revision > 9007199254740991 || strconv.FormatInt(revision, 10) != resource.Revision {
		return false
	}
	data := resource.Data
	if resource.ID != workflowID || data.ID != workflowID || data.Workflow != workflowID || data.Tenant != tenantID || data.Project == uuid.Nil || data.Type != "interchange_jobs" || data.Revision != resource.Revision || data.Created.IsZero() || data.Updated.IsZero() || data.Updated.Before(data.Created) {
		return false
	}
	if data.Direction != "import" && data.Direction != "export" {
		return false
	}
	if !supportedParseFormat(data.Format) {
		return false
	}
	for _, id := range []*uuid.UUID{data.Mapping, data.Report, data.Bundle} {
		if id != nil && *id == uuid.Nil {
			return false
		}
	}
	if resource.Document != nil && (resource.Document.ArtifactID == uuid.Nil || !validImportRef(*resource.Document)) {
		return false
	}
	return true
}

func validInterchangeInspection(raw []byte, tenantID, workflowID uuid.UUID) bool {
	if len(raw) < 2 || len(raw) > 1<<20 || tenantID == uuid.Nil || workflowID == uuid.Nil {
		return false
	}
	fields, ok := closedInterchangeObject(raw)
	if !ok || len(fields) != 11 {
		return false
	}
	for _, key := range []string{"schema_version", "workflow_id", "tenant_id", "project_id", "run_id", "revision", "status", "stage", "ingest_run_id", "collection_id", "review"} {
		if _, exists := fields[key]; !exists {
			return false
		}
	}
	var value struct {
		SchemaVersion int             `json:"schema_version"`
		Workflow      uuid.UUID       `json:"workflow_id"`
		Tenant        uuid.UUID       `json:"tenant_id"`
		Project       uuid.UUID       `json:"project_id"`
		Run           uuid.UUID       `json:"run_id"`
		Revision      string          `json:"revision"`
		Status        string          `json:"status"`
		Stage         string          `json:"stage"`
		Ingest        *uuid.UUID      `json:"ingest_run_id"`
		Collection    *uuid.UUID      `json:"collection_id"`
		Review        json.RawMessage `json:"review"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF {
		return false
	}
	revision, err := strconv.ParseInt(value.Revision, 10, 64)
	if err != nil || revision < 1 || revision > 9007199254740991 || strconv.FormatInt(revision, 10) != value.Revision {
		return false
	}
	if value.SchemaVersion != 1 || value.Workflow != workflowID || value.Tenant != tenantID || value.Project == uuid.Nil || value.Run == uuid.Nil {
		return false
	}
	for _, id := range []*uuid.UUID{value.Ingest, value.Collection} {
		if id != nil && *id == uuid.Nil {
			return false
		}
	}
	expected := value.Status
	switch value.Status {
	case "queued", "cancelling", "failed", "cancelled":
	case "completed", "completed_with_errors":
		expected = "completed"
	case "waiting_review":
		expected = "review"
	case "running":
		switch value.Stage {
		case "parsing", "ingesting", "preparing_import", "exporting", "finalizing":
		default:
			return false
		}
		expected = value.Stage
	default:
		return false
	}
	if value.Stage != expected {
		return false
	}
	if value.Stage == "exporting" || value.Stage == "finalizing" {
		if value.Ingest != nil || value.Collection != nil || string(value.Review) != "null" {
			return false
		}
	}
	if string(value.Review) == "null" {
		return true
	}
	review, ok := closedInterchangeObject(value.Review)
	if !ok || len(review) != 6 {
		return false
	}
	for _, key := range []string{"report", "proposed_count", "entry_count", "can_close", "can_decide", "targets"} {
		if _, exists := review[key]; !exists {
			return false
		}
	}
	for _, key := range []string{"proposed_count", "entry_count"} {
		var number *int
		if json.Unmarshal(review[key], &number) != nil || number == nil {
			return false
		}
	}
	for _, key := range []string{"can_close", "can_decide"} {
		var flag *bool
		if json.Unmarshal(review[key], &flag) != nil || flag == nil {
			return false
		}
	}
	if len(review["targets"]) < 2 || review["targets"][0] != '[' {
		return false
	}
	report, ok := closedInterchangeObject(review["report"])
	if !ok || len(report) != 2 {
		return false
	}
	for _, key := range []string{"artifact_id", "sha256"} {
		if _, exists := report[key]; !exists {
			return false
		}
	}
	var detail struct {
		Report struct {
			ArtifactID uuid.UUID `json:"artifact_id"`
			SHA256     string    `json:"sha256"`
		} `json:"report"`
		Proposed  int               `json:"proposed_count"`
		Entries   int               `json:"entry_count"`
		CanClose  bool              `json:"can_close"`
		CanDecide bool              `json:"can_decide"`
		Targets   []json.RawMessage `json:"targets"`
	}
	decoder = json.NewDecoder(bytes.NewReader(value.Review))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&detail) != nil || decoder.Decode(new(any)) != io.EOF {
		return false
	}
	if detail.Report.ArtifactID == uuid.Nil || len(detail.Report.SHA256) != 64 || detail.Entries < 1 || detail.Entries > 5000 || detail.Proposed < 0 || detail.Proposed > detail.Entries || len(detail.Targets) > 100 || len(detail.Targets) > detail.Proposed {
		return false
	}
	for _, c := range detail.Report.SHA256 {
		if c < '0' || c > '9' && c < 'a' || c > 'f' {
			return false
		}
	}
	if (detail.CanClose && (value.Status != "waiting_review" || detail.Proposed != 0)) || (detail.CanDecide && value.Status != "waiting_review") {
		return false
	}
	seen := make(map[uuid.UUID]struct{}, len(detail.Targets))
	for _, rawTarget := range detail.Targets {
		target, ok := closedInterchangeObject(rawTarget)
		if !ok || len(target) != 5 {
			return false
		}
		for _, key := range []string{"entry_id", "external_id", "asset_version_id", "state", "fields"} {
			if _, exists := target[key]; !exists {
				return false
			}
		}
		if len(target["fields"]) < 2 || target["fields"][0] != '[' {
			return false
		}
		var item struct {
			Entry    uuid.UUID         `json:"entry_id"`
			External string            `json:"external_id"`
			Asset    uuid.UUID         `json:"asset_version_id"`
			State    string            `json:"state"`
			Fields   []json.RawMessage `json:"fields"`
		}
		decoder = json.NewDecoder(bytes.NewReader(rawTarget))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&item) != nil || decoder.Decode(new(any)) != io.EOF {
			return false
		}
		if item.Entry == uuid.Nil || item.Asset == uuid.Nil || item.State != "proposed" || len(item.External) < 1 || len(item.External) > 240 || len(item.Fields) > 128 {
			return false
		}
		if _, exists := seen[item.Entry]; exists {
			return false
		}
		seen[item.Entry] = struct{}{}
		fieldNames := make(map[string]struct{}, len(item.Fields))
		for _, rawField := range item.Fields {
			field, ok := closedInterchangeObject(rawField)
			if !ok || len(field) != 3 {
				return false
			}
			for _, key := range []string{"name", "status", "reason"} {
				if _, exists := field[key]; !exists {
					return false
				}
			}
			var attribute struct {
				Name   string  `json:"name"`
				Status string  `json:"status"`
				Reason *string `json:"reason"`
			}
			decoder = json.NewDecoder(bytes.NewReader(rawField))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&attribute) != nil || decoder.Decode(new(any)) != io.EOF {
				return false
			}
			if len(attribute.Name) < 1 || len(attribute.Name) > 80 {
				return false
			}
			if _, exists := fieldNames[attribute.Name]; exists {
				return false
			}
			fieldNames[attribute.Name] = struct{}{}
			switch attribute.Status {
			case "exact":
				if attribute.Reason != nil {
					return false
				}
			case "unsupported", "rejected":
				if attribute.Reason == nil || len(*attribute.Reason) < 1 || len(*attribute.Reason) > 200 {
					return false
				}
			default:
				return false
			}
		}
	}
	return true
}
