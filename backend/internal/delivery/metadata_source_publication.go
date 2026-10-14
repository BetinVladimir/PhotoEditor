package delivery

import (
	"bytes"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// MetadataSourcePublicationScope is supplied by authenticated API admission.
// Original lineage and WorkingImage/Canvas bytes must be independently resolved.
// Compiling this envelope grants neither approval nor ready artifact state.
type MetadataSourcePublicationScope struct {
	Tenant        uuid.UUID                     `json:"tenant_id"`
	Project       uuid.UUID                     `json:"project_id"`
	Actor         uuid.UUID                     `json:"actor_id"`
	Session       uuid.UUID                     `json:"session_id"`
	Binding       v2input.SessionVersionBinding `json:"binding"`
	Original      v2input.Ref                   `json:"original"`
	OriginalBytes int64                         `json:"original_bytes"`
	OriginalMIME  string                        `json:"original_mime"`
}

type metadataSourcePublicationEnvelope struct {
	SchemaVersion  int                            `json:"schema_version"`
	Scope          MetadataSourcePublicationScope `json:"scope"`
	MetadataSource v2input.Ref                    `json:"metadata_source"`
	MetadataJCS    string                         `json:"metadata_jcs"`
	Policy         string                         `json:"policy"`
	ApprovedFields []string                       `json:"approved_fields"`
	FrozenJCS      string                         `json:"frozen_jcs"`
	FrozenSHA256   string                         `json:"frozen_sha256"`
}

// MetadataSourcePublication owns all canonical bytes required by future SQL
// reservation/completion. Accessors cannot mutate the pinned admission envelope.
type MetadataSourcePublication struct {
	body   []byte
	source []byte
	frozen []byte
	ref    v2input.Ref
	pin    string
}

func (p MetadataSourcePublication) JCS() []byte       { return append([]byte(nil), p.body...) }
func (p MetadataSourcePublication) SourceJCS() []byte { return append([]byte(nil), p.source...) }
func (p MetadataSourcePublication) FrozenJCS() []byte { return append([]byte(nil), p.frozen...) }
func (p MetadataSourcePublication) Ref() v2input.Ref  { return p.ref }
func (p MetadataSourcePublication) SHA256() string    { return p.pin }

func CompileMetadataSourcePublication(scope MetadataSourcePublicationScope, ref v2input.Ref, source []byte,
	policy string, approved []string) (MetadataSourcePublication, error) {
	var zero MetadataSourcePublication
	if scope.Tenant == uuid.Nil || scope.Project == uuid.Nil || scope.Actor == uuid.Nil || scope.Session == uuid.Nil ||
		scope.Original.ArtifactID == uuid.Nil || !shaPattern.MatchString(scope.Original.SHA256) ||
		scope.Original.ArtifactID == scope.Binding.Source.ArtifactID || scope.Original.ArtifactID == ref.ArtifactID ||
		scope.OriginalBytes < 1 || scope.OriginalBytes > 2*1024*1024*1024 {
		return zero, v2input.ErrInvalid
	}
	switch scope.OriginalMIME {
	case "image/jpeg", "image/png", "image/tiff",
		"image/x-adobe-dng", "image/x-canon-cr2", "image/x-nikon-nef", "image/x-sony-arw",
		"image/x-fuji-raf", "image/x-olympus-orf", "image/x-panasonic-rw2":
	default:
		return zero, v2input.ErrInvalid
	}
	// Freeze validates exact source canonical bytes/SHA, version/canvas/ref identity,
	// bounded document fields, policy and a closed unique explicit approval list.
	frozen, frozenPin, err := FreezeDeliveryMetadata(scope.Binding, ref, source, policy, approved)
	if err != nil {
		return zero, err
	}
	fields := append([]string{}, approved...)
	sort.Strings(fields)
	body, err := canonical.Marshal(metadataSourcePublicationEnvelope{1, scope, ref, string(source), policy, fields, string(frozen), frozenPin})
	if err != nil {
		return zero, err
	}
	if len(body) > 1048576 {
		return zero, v2input.ErrInvalid
	}
	return MetadataSourcePublication{body, append([]byte(nil), source...), frozen, ref, digest(body)}, nil
}

// Restore compares with external current scope/policy/approval, never values
// chosen by the envelope itself. SQL must still repeat live authorization.
func RestoreMetadataSourcePublication(raw []byte, pin string, scope MetadataSourcePublicationScope,
	ref v2input.Ref, source []byte, policy string, approved []string) (MetadataSourcePublication, error) {
	var zero MetadataSourcePublication
	if len(raw) < 2 || len(raw) > 1048576 || !shaPattern.MatchString(pin) || digest(raw) != pin {
		return zero, v2input.ErrInvalid
	}
	expected, err := CompileMetadataSourcePublication(scope, ref, source, policy, approved)
	if err != nil {
		return zero, err
	}
	if !bytes.Equal(raw, expected.body) {
		return zero, v2input.ErrInvalid
	}
	return expected, nil
}
