package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"io"
	"math"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"strings"
	"time"
)

type OperatorMaskUse struct {
	Role      string  `json:"role"`
	BindingID string  `json:"binding_id"`
	Purpose   string  `json:"purpose"`
	Strength  float64 `json:"strength"`
}
type OperatorMaskApproval struct {
	Challenge    json.RawMessage            `json:"challenge"`
	Task         uuid.UUID                  `json:"task_id"`
	Item         uuid.UUID                  `json:"item_id"`
	Version      uuid.UUID                  `json:"asset_version_id"`
	InputSHA     string                     `json:"input_sha256"`
	ChallengeSHA string                     `json:"challenge_sha256"`
	Actor        uuid.UUID                  `json:"actor_id"`
	Bindings     []applyOverrideMaskBinding `json:"bindings"`
}

// Approval follows measurement. Neither corpus goldens nor requested model
// names are accepted replies, and no SQL Override/decision receipt is emitted.
func ApproveOperatorMask(ctx context.Context, request OperatorInteractiveMask, measured OperatorMeasuredTask, ledger *OperatorApplyLedger, objects *OperatorPhysicalObjects, actor uuid.UUID, directory string) (OperatorMaskApproval, error) {
	var zero OperatorMaskApproval
	if actor == uuid.Nil || len(request.Uses) == 0 || len(request.Uses) > 128 || measured.Task.ID != request.Task {
		return zero, errors.New("mask approval request invalid")
	}
	var source *applyInput
	for i := range ledger.inputs {
		if ledger.inputs[i].ItemID == request.Item {
			source = &ledger.inputs[i]
			break
		}
	}
	if source == nil {
		return zero, errors.New("mask approval source missing")
	}
	bindings := []applyOverrideMaskBinding{}
	seen := map[string]bool{}
	for _, use := range request.Uses {
		if (use.Role != "prompt_mask_ref" && use.Role != "mask_target_ref" && use.Role != "mask_guard_ref") || use.BindingID == "" || len(use.BindingID) > 120 || math.IsNaN(use.Strength) || math.IsInf(use.Strength, 0) || use.Strength < 0 || use.Strength > 1 {
			return zero, errors.New("mask requested use invalid")
		}
		if use.Purpose != "protected" && use.Purpose != "region_adjustment" && use.Purpose != "inpaint_target" && use.Purpose != "generation_target" && !strings.HasPrefix(use.Purpose, "portrait_") {
			return zero, errors.New("mask use unsupported")
		}
		key := use.BindingID
		if seen[key] {
			return zero, errors.New("duplicate mask use")
		}
		seen[key] = true
		var root *styleArtifact
		for _, output := range measured.Outputs {
			if output.Role == use.Role {
				if root != nil {
					return zero, errors.New("duplicate measured mask root")
				}
				copy := output.Artifact
				root = &copy
			}
		}
		if root == nil {
			return zero, errors.New("measured mask root missing")
		}
		body, err := readApplyJSON(ctx, objects, "operator-measurement", *root)
		if err != nil {
			return zero, err
		}
		mask, err := review.ParseMaskRef(body)
		if err != nil {
			return zero, err
		}
		if mask.Canvas != source.Working.Canvas || !bytes.Equal(mask.TransformChain, source.Working.TransformChain) || mask.Protected != (use.Purpose == "protected") || mask.Protected != (use.Role == "mask_guard_ref") {
			return zero, errors.New("mask guard/source binding changed")
		}
		pixel, exists := ledger.objects[mask.Artifact.ID]
		if !exists || pixel.SHA256 != mask.Artifact.SHA256 {
			return zero, errors.New("mask pixels not measured")
		}
		bindings = append(bindings, applyOverrideMaskBinding{BindingID: use.BindingID, Purpose: use.Purpose, Mask: mask, Confirmed: !mask.Protected, Strength: use.Strength})
	}
	challenge := struct {
		Schema         int                        `json:"schema_version"`
		Boundary       string                     `json:"boundary"`
		Actor          uuid.UUID                  `json:"actor_id"`
		Tenant         uuid.UUID                  `json:"tenant_id"`
		Run            uuid.UUID                  `json:"run_id"`
		Request        OperatorInteractiveMask    `json:"request"`
		Task           PreparedTask               `json:"task"`
		ObservationSHA string                     `json:"observation_sha256"`
		Bindings       []applyOverrideMaskBinding `json:"bindings"`
	}{1, "operator_mask_use_approval_not_sql_receipt", actor, ledger.lease.TenantID, ledger.lease.RunID, request, measured.Task, measured.ObservationSHA256, bindings}
	raw, err := canonical.Marshal(challenge)
	if err != nil {
		return zero, err
	}
	pin, err := canonical.Hash(json.RawMessage(raw))
	if err != nil {
		return zero, err
	}
	if err = writeOperatorExclusive(filepath.Join(directory, "mask-"+request.Task.String()+"-challenge.json"), raw); err != nil {
		return zero, err
	}
	replyPath := filepath.Join(directory, "mask-"+request.Task.String()+"-approval.json")
	for {
		if err = ctx.Err(); err != nil {
			return zero, err
		}
		info, statErr := os.Lstat(replyPath)
		if errors.Is(statErr, os.ErrNotExist) {
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return zero, ctx.Err()
			case <-timer.C:
			}
			continue
		}
		if statErr != nil {
			return zero, statErr
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() < 2 || info.Size() > 8192 {
			return zero, errors.New("private bounded mask approval required")
		}
		file, openErr := os.Open(replyPath)
		if openErr != nil {
			return zero, openErr
		}
		reply, readErr := io.ReadAll(io.LimitReader(file, 8193))
		closeErr := file.Close()
		if readErr != nil {
			return zero, readErr
		}
		if closeErr != nil {
			return zero, closeErr
		}
		var approved struct {
			Schema       int       `json:"schema_version"`
			Actor        uuid.UUID `json:"actor_id"`
			ChallengeSHA string    `json:"challenge_sha256"`
			Approved     bool      `json:"approved"`
		}
		decoder := json.NewDecoder(bytes.NewReader(reply))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&approved) != nil || decoder.Decode(new(any)) != io.EOF || approved.Schema != 1 || approved.Actor != actor || approved.ChallengeSHA != pin || !approved.Approved {
			return zero, errors.New("explicit mask approval refused or stale")
		}
		return OperatorMaskApproval{Challenge: raw, Task: request.Task, Item: request.Item, Version: source.VersionID, InputSHA: source.Image.SHA256, ChallengeSHA: pin, Actor: actor, Bindings: bindings}, nil
	}
}

func bindOperatorMaskInputs(ledger *OperatorApplyLedger, approvals []OperatorMaskApproval) error {
	if len(approvals) == 0 {
		return nil
	}
	grouped := map[uuid.UUID][]applyOverrideMaskBinding{}
	for _, approval := range approvals {
		grouped[approval.Item] = append(grouped[approval.Item], approval.Bindings...)
	}
	inputs := []applyItemMaskInput{}
	for _, source := range ledger.inputs {
		masks := grouped[source.ItemID]
		if len(masks) == 0 {
			return errors.New("whole source mask approval required")
		}
		body, err := canonical.Marshal(map[string]any{"schema_version": 1, "revision": 1, "input_sha256": source.Image.SHA256, "modules": map[string]any{"strength": 1}, "mask_refs": masks})
		if err != nil {
			return err
		}
		pin, err := canonical.Hash(json.RawMessage(body))
		if err != nil {
			return err
		}
		inputs = append(inputs, applyItemMaskInput{ItemID: source.ItemID, AssetVersionID: source.VersionID, InputSHA: source.Image.SHA256, BodySHA: pin, Body: body})
	}
	if _, err := boundApplyItemMasks(ledger.inputs, inputs); err != nil {
		return err
	}
	for i := range ledger.inputs {
		var cfg map[string]json.RawMessage
		if json.Unmarshal(ledger.inputs[i].Config, &cfg) != nil {
			return errors.New("operator input config invalid")
		}
		if len(cfg["item_mask_inputs"]) != 0 {
			return errors.New("preapproved mask input forbidden")
		}
		raw, err := canonical.Marshal(inputs)
		if err != nil {
			return err
		}
		cfg["item_mask_inputs"] = raw
		raw, err = canonical.Marshal(cfg)
		if err != nil {
			return err
		}
		ledger.inputs[i].Config = raw
	}
	return nil
}
