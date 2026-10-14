package interchange

import (
	"context"
	"encoding/json"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// The archive may contain many resolved styles. Only an exact old source and
// a fully qualified typed parent can witness a LUT occurrence.
type ImportArchivedLUTGeometrySource struct {
	Parsed    []byte
	Reader    ImportArchiveDocumentReader
	Qualifier ImportResolvedQualifier
	Old       v2input.SessionVersionBinding
}

func (s ImportArchivedLUTGeometrySource) ResolveExportArrayGeometry(ctx context.Context, entry v2input.Entry) (ExportArrayGeometry, error) {
	var zero ExportArrayGeometry
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if entry.Contract != "AbsoluteLUTArray" || !validImportRef(entry.Ref) || s.Reader == nil || s.Qualifier == nil || !validImportRef(s.Old.Source) {
		return zero, v2input.ErrInvalid
	}
	if _, err := validateParsedImportDocument(ctx, s.Parsed); err != nil {
		return zero, err
	}
	var parsed importParsedDocument
	if err := decodeImportDocument(s.Parsed, &parsed); err != nil {
		return zero, err
	}
	files := map[string]v2input.Ref{}
	for _, file := range parsed.Files {
		files[file.Path] = file.Content
	}
	found := false
	var geometry ExportArrayGeometry
	for _, item := range parsed.Manifest.Inventory {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		if item.Contract != "ResolvedStyle" {
			continue
		}
		staged := files[item.Path]
		if !validImportRef(staged) || staged.SHA256 != item.SHA {
			return zero, v2input.ErrInvalid
		}
		raw, err := s.Reader.ReadImportArchiveDocument(ctx, staged, 8<<20)
		if err != nil {
			return zero, err
		}
		if len(raw) < 2 || len(raw) > 8<<20 || rawSHA(raw) != item.SHA {
			return zero, v2input.ErrInvalid
		}
		var projection struct {
			Params struct {
				Image v2input.Ref `json:"image"`
			} `json:"params"`
			LUT *struct {
				Artifact v2input.Ref `json:"artifact"`
				Size     int         `json:"size"`
				DType    string      `json:"dtype"`
				Order    string      `json:"order"`
				Profile  string      `json:"profile_id"`
			} `json:"absolute_lut"`
		}
		if err := json.Unmarshal(raw, &projection); err != nil {
			return zero, err
		}
		if projection.Params.Image != s.Old.Source || projection.LUT == nil || projection.LUT.Artifact.ArtifactID != entry.Ref.ArtifactID {
			continue
		}
		if projection.LUT.Artifact != entry.Ref || projection.LUT.DType != "float32" || projection.LUT.Order != "RGB_C" || projection.LUT.Profile != "work_rec2020_gamma24_v1" || (projection.LUT.Size != 33 && projection.LUT.Size != 65) {
			return zero, v2input.ErrInvalid
		}
		if err := s.Qualifier.ValidateArchivedImportResolved(ctx, append([]byte(nil), raw...), s.Old); err != nil {
			return zero, err
		}
		parent := v2input.Entry{Ref: v2input.Ref{ArtifactID: item.ID, SHA256: item.SHA}, Contract: "ResolvedStyle"}
		children, err := exportResolvedChildren(parent, raw)
		if err != nil {
			return zero, err
		}
		edge := false
		for _, child := range children {
			if child.Ref.ArtifactID == entry.Ref.ArtifactID {
				if child != entry {
					return zero, v2input.ErrInvalid
				}
				edge = true
			}
		}
		if !edge {
			return zero, v2input.ErrInvalid
		}
		digest, err := canonical.Hash(projection.LUT)
		if err != nil {
			return zero, err
		}
		current := ExportArrayGeometry{Width: projection.LUT.Size, Height: projection.LUT.Size, CanvasSHA: digest, DType: "float32"}
		if _, err := exportArrayMaximumBytes(entry, current); err != nil {
			return zero, err
		}
		if found && geometry != current {
			return zero, v2input.ErrInvalid
		}
		found = true
		geometry = current
	}
	if !found {
		return zero, v2input.ErrInvalid
	}
	return geometry, ctx.Err()
}

var _ ExportArrayGeometrySource = ImportArchivedLUTGeometrySource{}

// Returns one witnessed parent identity for the SQL occurrence certificate.
// ResolveExportArrayGeometry has already checked every matching parent agrees.
func (s ImportArchivedLUTGeometrySource) ResolveImportLUTParent(ctx context.Context, entry v2input.Entry) (v2input.Ref, error) {
	if _, err := s.ResolveExportArrayGeometry(ctx, entry); err != nil {
		return v2input.Ref{}, err
	}
	var parsed importParsedDocument
	if err := decodeImportDocument(s.Parsed, &parsed); err != nil {
		return v2input.Ref{}, err
	}
	files := map[string]v2input.Ref{}
	for _, file := range parsed.Files {
		files[file.Path] = file.Content
	}
	for _, item := range parsed.Manifest.Inventory {
		if err := ctx.Err(); err != nil {
			return v2input.Ref{}, err
		}
		if item.Contract != "ResolvedStyle" {
			continue
		}
		raw, err := s.Reader.ReadImportArchiveDocument(ctx, files[item.Path], 8<<20)
		if err != nil {
			return v2input.Ref{}, err
		}
		parent := v2input.Ref{ArtifactID: item.ID, SHA256: item.SHA}
		children, err := exportResolvedChildren(v2input.Entry{Ref: parent, Contract: "ResolvedStyle"}, raw)
		if err != nil {
			return v2input.Ref{}, err
		}
		for _, child := range children {
			if child == entry {
				var scope struct {
					Params struct {
						Image v2input.Ref `json:"image"`
					} `json:"params"`
				}
				if err := json.Unmarshal(raw, &scope); err != nil || scope.Params.Image != s.Old.Source {
					continue
				}
				if err := s.Qualifier.ValidateArchivedImportResolved(ctx, append([]byte(nil), raw...), s.Old); err != nil {
					return v2input.Ref{}, err
				}
				return parent, ctx.Err()
			}
		}
	}
	return v2input.Ref{}, v2input.ErrInvalid
}
