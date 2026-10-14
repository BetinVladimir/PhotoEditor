package v2draft

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
	"strconv"
	"strings"
)

type donorParamsIntent struct {
	Actor         uuid.UUID                             `json:"actor_id"`
	Session       uuid.UUID                             `json:"auth_session_id"`
	NodeKey       string                                `json:"node_key"`
	Owner         uuid.UUID                             `json:"owner_id"`
	Params        string                                `json:"params_jcs"`
	ParamsSHA     string                                `json:"params_sha"`
	Qualification v2portrait.DonorQualificationPin      `json:"qualification"`
	Objects       []v2portrait.DonorQualificationObject `json:"qualification_objects"`
	ReservedBytes int64                                 `json:"reserved_bytes"`
}

// DonorParamsRecoverySeed describes pinned inputs to reload through authorized
// catalogs. Parsing does not verify report/calibration bytes or grant execution.
type DonorParamsRecoverySeed struct {
	intent  donorParamsIntent
	params  v2portrait.OpenEyesParams
	ordinal int
}

func (s DonorParamsRecoverySeed) Params() v2portrait.OpenEyesParams { return s.params }
func (s DonorParamsRecoverySeed) Ordinal() int                      { return s.ordinal }
func (s DonorParamsRecoverySeed) Owner() uuid.UUID                  { return s.intent.Owner }
func (s DonorParamsRecoverySeed) QualificationPin() v2portrait.DonorQualificationPin {
	return s.intent.Qualification
}
func (s DonorParamsRecoverySeed) QualificationObjects() []v2portrait.DonorQualificationObject {
	return append([]v2portrait.DonorQualificationObject(nil), s.intent.Objects...)
}

// ParseDonorParamsRecoveryIntent accepts exact canonical typed intent for the
// authenticated actor/session and deterministic task of the requested workflow.
func ParseDonorParamsRecoveryIntent(ctx context.Context, who auth.Identity, workflow uuid.UUID, raw []byte) (DonorParamsRecoverySeed, error) {
	var zero DonorParamsRecoverySeed
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if who.UserID == uuid.Nil || who.SessionID == uuid.Nil || workflow == uuid.Nil || len(raw) < 2 || len(raw) > 1<<20 {
		return zero, v2input.ErrInvalid
	}
	owned := append([]byte(nil), raw...)
	normal, err := canonical.Bytes(owned)
	if err != nil || !bytes.Equal(normal, owned) {
		return zero, v2input.ErrInvalid
	}
	var intent donorParamsIntent
	if json.Unmarshal(owned, &intent) != nil {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(intent)
	if err != nil || !bytes.Equal(exact, owned) || intent.Actor != who.UserID || intent.Session != who.SessionID || intent.ReservedBytes < 1 || intent.ReservedBytes > 9007199254740991 {
		return zero, v2input.ErrInvalid
	}
	if !strings.HasPrefix(intent.NodeKey, "portrait.open_eyes:") {
		return zero, v2input.ErrInvalid
	}
	ordinal, err := strconv.Atoi(strings.TrimPrefix(intent.NodeKey, "portrait.open_eyes:"))
	if err != nil || ordinal < 0 || ordinal >= 5000 {
		return zero, v2input.ErrInvalid
	}
	key, err := v2input.NodeKey("portrait.open_eyes", ordinal)
	if err != nil || key != intent.NodeKey {
		return zero, v2input.ErrInvalid
	}
	owner, err := v2input.TaskID(workflow, "portrait.open_eyes", ordinal)
	if err != nil || owner != intent.Owner {
		return zero, v2input.ErrInvalid
	}
	var params v2portrait.OpenEyesParams
	if len(intent.Params) < 2 || len(intent.Params) > 65536 || json.Unmarshal([]byte(intent.Params), &params) != nil {
		return DonorParamsRecoverySeed{}, v2input.ErrInvalid
	}
	body, digest, err := params.Compile()
	if err != nil || string(body) != intent.Params || digest != intent.ParamsSHA {
		return DonorParamsRecoverySeed{}, v2input.ErrInvalid
	}
	inputs, err := v2portrait.DonorDirectInputs(params)
	if err != nil {
		return DonorParamsRecoverySeed{}, err
	}
	pin := intent.Qualification
	if _, err = pin.Compile(); err != nil {
		return DonorParamsRecoverySeed{}, err
	}
	if len(intent.Objects) != 2 {
		return DonorParamsRecoverySeed{}, v2input.ErrInvalid
	}
	refs := []v2input.Ref{pin.Calibration, pin.Report}
	contracts := []string{"Calibration", "Report"}
	for i, obj := range intent.Objects {
		maxBytes := int64(1 << 20)
		if i == 1 {
			maxBytes = 8 << 20
		}
		if obj.Contract != contracts[i] || obj.ArtifactID != refs[i].ArtifactID || obj.SHA256 != refs[i].SHA256 || len(obj.Bucket) < 1 || len(obj.Bucket) > 255 || len(obj.Key) < 1 || len(obj.Key) > 1024 || len(obj.Version) < 1 || len(obj.Version) > 1024 || obj.Version == "null" || obj.Bytes < 2 || obj.Bytes > maxBytes {
			return DonorParamsRecoverySeed{}, v2input.ErrInvalid
		}
		for _, ref := range inputs {
			if ref.ArtifactID == obj.ArtifactID {
				return DonorParamsRecoverySeed{}, v2input.ErrInvalid
			}
		}
	}
	if err = ctx.Err(); err != nil {
		return DonorParamsRecoverySeed{}, err
	}
	return DonorParamsRecoverySeed{intent: intent, params: params, ordinal: ordinal}, nil
}
