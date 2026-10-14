package delivery

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/exportpreset"
	"photoeditor/backend/internal/v2input"
)

type ConfigProof struct {
	PresetID  uuid.UUID `json:"preset_id"`
	ConfigJCS string    `json:"config_jcs"`
}
type PrepareProof struct {
	SchemaVersion  int           `json:"schema_version"`
	ProjectID      uuid.UUID     `json:"project_id"`
	AuthSessionID  uuid.UUID     `json:"auth_session_id"`
	FrozenInputJCS string        `json:"frozen_input_jcs"`
	SnapshotJCS    string        `json:"snapshot_jcs"`
	ConfigsJCS     []ConfigProof `json:"configs_jcs"`
}
type PrepareParams struct {
	SchemaVersion int         `json:"schema_version"`
	Input         v2input.Ref `json:"input"`
}

// CompilePrepareProof repeats the same frozen compiler before preparing durable
// recovery bytes. Caller must separately supply project/actor/closure authority.
func CompilePrepareProof(project, session uuid.UUID, frozenRef v2input.Ref, frozenRaw, snapshotRaw []byte, configs []PresetContent) (PrepareProof, []byte, []byte, error) {
 return compilePrepareProof(project,session,frozenRef,frozenRaw,snapshotRaw,configs,false)
}
func compilePrepareProof(project,session uuid.UUID,frozenRef v2input.Ref,frozenRaw,snapshotRaw []byte,configs []PresetContent,watermark bool)(PrepareProof,[]byte,[]byte,error){
	invalid := func() (PrepareProof, []byte, []byte, error) {
		return PrepareProof{}, nil, nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	if project == uuid.Nil || session == uuid.Nil || frozenRef.ArtifactID == uuid.Nil || len(frozenRaw) > 1<<20 || digest(frozenRaw) != frozenRef.SHA256 {
		return invalid()
	}
	var frozen FrozenInput
	if json.Unmarshal(frozenRaw, &frozen) != nil {
		return invalid()
	}
	_, exact, _, err := CompileFrozenInput(frozen.Snapshot, snapshotRaw, frozen.Policy, configs)
	if err != nil {
		return PrepareProof{}, nil, nil, err
	}
	if !bytes.Equal(exact, frozenRaw) {
		return invalid()
	}
	inventory := map[uuid.UUID]PresetContent{}
	for _, config := range configs {
		inventory[config.ID] = config
	}
	proof := PrepareProof{1, project, session, string(frozenRaw), string(snapshotRaw), make([]ConfigProof, 0, len(configs))}
	for _, preset := range frozen.Presets {
		item := inventory[preset.PresetID]
		config, err := exportpreset.DecodeFrozenConfig(item.Raw, item.SHA)
		if err != nil {
			return invalid()
		}
		if config.Watermark != nil && !watermark {
			return PrepareProof{}, nil, nil, domain.NewError("DELIVERY_WATERMARK_UNAVAILABLE", 503)
		}
		proof.ConfigsJCS = append(proof.ConfigsJCS, ConfigProof{preset.PresetID, string(item.Raw)})
	}
	raw, err := canonical.Marshal(proof)
	if err != nil || len(raw) > 16<<20 {
		return invalid()
	}
	params, err := canonical.Marshal(PrepareParams{1, frozenRef})
	if err != nil {
		return invalid()
	}
	return proof, raw, params, nil
}
