package provenance

import (
	"bytes"
	"context"
	"regexp"
	"slices"
	"unicode/utf8"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type signingMetadataParams struct {
	Encoded  v2input.Ref `json:"encoded"`
	Document v2input.Ref `json:"document"`
	Policy   string      `json:"policy"`
	CopyGPS  bool        `json:"copy_gps"`
	ToolID   string      `json:"tool_id"`
}
type signingMetadataGPS struct {
	Latitude  float64  `json:"latitude"`
	Longitude float64  `json:"longitude"`
	Altitude  *float64 `json:"altitude_m"`
}
type signingMetadataDocument struct {
	Author    *string             `json:"author"`
	Copyright *string             `json:"copyright"`
	Make      *string             `json:"camera_make"`
	Model     *string             `json:"camera_model"`
	Lens      *string             `json:"lens"`
	DateTime  *string             `json:"datetime_original"`
	GPS       *signingMetadataGPS `json:"gps"`
}
type signingEncodedImage struct {
	Artifact v2input.Ref `json:"artifact"`
	Format   string      `json:"format"`
	Width    int64       `json:"width"`
	Height   int64       `json:"height"`
	ICCID    string      `json:"icc_id"`
	ICCSHA   string      `json:"icc_sha256"`
	Alpha    bool        `json:"alpha"`
	Policy   string      `json:"metadata_policy"`
}
type signingMetadataReport struct {
	Encoded         v2input.Ref `json:"encoded"`
	Document        v2input.Ref `json:"document"`
	Policy          string      `json:"policy"`
	Copied          []string    `json:"copied_fields"`
	Orientation     int         `json:"orientation"`
	PreviewsRemoved bool        `json:"stale_previews_removed"`
	ICCPreserved    bool        `json:"icc_preserved"`
}

// MetadataSigningTaskResolver checks adopted task/document claims. The worker
// independently proves physical pixel/ICC preservation before canonical commit;
// report booleans here do not replace that physical verification.
type MetadataSigningTaskResolver struct {
	Documents SigningTaskDocumentReader
	Producers SigningImageProducer
}

func (s MetadataSigningTaskResolver) ResolveSigningTaskLineage(ctx context.Context, who auth.Identity, ready ReadySigningExport, e CanonicalSigningTask) (ResolvedSigningTask, error) {
	var zero ResolvedSigningTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Documents == nil || s.Producers == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || ready.Tenant == uuid.Nil || ready.Project == uuid.Nil || ready.ExportID == uuid.Nil || e.Task != ready.Task || e.Run != ready.Run || e.Operation != "metadata.write" || e.Version != "1" || e.ReviewRequired || normalizeCanonicalSigningTask(&e) != nil {
		return zero, ErrInvalid
	}
	outputs, err := canonicalTaskOutputRefs(e.Outputs)
	if err != nil || len(outputs) != 3 || outputs[0] != ready.Ref {
		return zero, ErrInvalid
	}
	documents := map[v2input.Ref][]byte{}
	read := func(r v2input.Ref, out any) error {
		raw, err := s.Documents.ReadSigningTaskDocument(ctx, who, ready, e, r, 1<<20)
		if err != nil {
			return err
		}
		raw = append([]byte(nil), raw...)
		if !pinned(raw, r, 1<<20) || decode(raw, out, 1<<20) != nil {
			return ErrInvalid
		}
		documents[r] = raw
		return nil
	}
	var p signingMetadataParams
	if err := read(e.Params, &p); err != nil {
		return zero, err
	}
	if !ref(p.Encoded) || !ref(p.Document) || p.Encoded == p.Document || p.ToolID != "exiftool_v1" || (p.Policy != "copyright_only" && p.Policy != "safe_camera") || p.CopyGPS && p.Policy != "safe_camera" {
		return zero, ErrInvalid
	}
	var before, after signingEncodedImage
	var document signingMetadataDocument
	var report signingMetadataReport
	if err := read(p.Encoded, &before); err != nil {
		return zero, err
	}
	if !validSigningEncodedImage(before) || before.Policy != "strip" {
		return zero, ErrInvalid
	}
	mime := map[string]string{"jpeg": "image/jpeg", "png": "image/png"}[before.Format]
	if mime == "" || mime != ready.Object.ContentType {
		return zero, ErrInvalid
	}
	inputs, err := canonicalTaskOutputRefs(e.Inputs)
	if err != nil || len(inputs) != 3 || inputs[0] != p.Encoded || inputs[1] != before.Artifact || inputs[2] != p.Document {
		return zero, ErrInvalid
	}
	if err := read(p.Document, &document); err != nil {
		return zero, err
	}
	copied, err := expectedSigningMetadataFields(document, p)
	if err != nil {
		return zero, err
	}
	if err := read(outputs[1], &after); err != nil {
		return zero, err
	}
	expected := before
	expected.Artifact = ready.Ref
	expected.Policy = p.Policy
	if after != expected || !validSigningEncodedImage(after) || after.Artifact == before.Artifact {
		return zero, ErrInvalid
	}
	if err := read(outputs[2], &report); err != nil {
		return zero, err
	}
	if report.Encoded != p.Encoded || report.Document != p.Document || report.Policy != p.Policy || report.Copied == nil || !slices.Equal(report.Copied, copied) || report.Orientation != 1 || !report.PreviewsRemoved || !report.ICCPreserved {
		return zero, ErrInvalid
	}
	producer, err := s.Producers.ResolveSigningImageProducer(ctx, who, ready, p.Encoded)
	if err != nil {
		return zero, err
	}
	if producer == uuid.Nil || producer == e.Task {
		return zero, ErrInvalid
	}
	for r, raw := range documents {
		current, err := s.Documents.ReadSigningTaskDocument(ctx, who, ready, e, r, 1<<20)
		if err != nil {
			return zero, err
		}
		if !bytes.Equal(current, raw) {
			return zero, ErrInvalid
		}
	}
	current, err := s.Producers.ResolveSigningImageProducer(ctx, who, ready, p.Encoded)
	if err != nil {
		return zero, err
	}
	if current != producer {
		return zero, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return ResolvedSigningTask{Node: SigningExecutionNode{Task: e.Task, Run: e.Run, CanonicalResult: e.Result, Attempt: e.Attempt, Fence: e.Fence, Parents: []uuid.UUID{producer}, Inputs: []v2input.Ref{p.Encoded}, Outputs: outputs, Action: SigningAction{Operation: e.Operation, Version: e.Version, ParamsSHA: e.Params.SHA256}}}, nil
}

func validSigningEncodedImage(image signingEncodedImage) bool {
	return ref(image.Artifact) && (image.Format == "jpeg" || image.Format == "png" || image.Format == "tiff") && image.Width > 0 && image.Width <= 200000000 && image.Height > 0 && image.Height <= 200000000/image.Width && (image.ICCID == "srgb_v1" || image.ICCID == "work_rec2020_gamma24_v1") && digest(image.ICCSHA) && (image.Policy == "strip" || image.Policy == "copyright_only" || image.Policy == "safe_camera") && !(image.Format == "jpeg" && image.Alpha)
}

var signingMetadataDate = regexp.MustCompile(`^[0-9]{4}:[0-9]{2}:[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}$`)

func expectedSigningMetadataFields(d signingMetadataDocument, p signingMetadataParams) ([]string, error) {
	copied := []string{}
	for _, field := range []struct {
		name   string
		value  *string
		camera bool
	}{{"author", d.Author, false}, {"copyright", d.Copyright, false}, {"camera_make", d.Make, true}, {"camera_model", d.Model, true}, {"lens", d.Lens, true}, {"datetime_original", d.DateTime, true}} {
		if field.value == nil {
			continue
		}
		if !utf8.ValidString(*field.value) || utf8.RuneCountInString(*field.value) < 1 || utf8.RuneCountInString(*field.value) > 256 {
			return nil, ErrInvalid
		}
		if field.name == "datetime_original" && !signingMetadataDate.MatchString(*field.value) {
			return nil, ErrInvalid
		}
		if !field.camera || p.Policy == "safe_camera" {
			copied = append(copied, field.name)
		}
	}
	if d.GPS != nil {
		if d.GPS.Latitude < -90 || d.GPS.Latitude > 90 || d.GPS.Longitude < -180 || d.GPS.Longitude > 180 || d.GPS.Altitude != nil && (*d.GPS.Altitude < -1000 || *d.GPS.Altitude > 100000) {
			return nil, ErrInvalid
		}
		if p.CopyGPS && p.Policy == "safe_camera" {
			copied = append(copied, "gps")
		}
	}
	return copied, nil
}

var _ SigningTaskLineageResolver = MetadataSigningTaskResolver{}
