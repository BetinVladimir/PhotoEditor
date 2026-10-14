package v2draft

import (
	"bytes"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

func operationParamRefs(raw []byte, path string, parent v2input.Ref) ([]LayerRef, bool, error) {
	children, handled, err := compositionParamRefs(raw, path, parent)
	if err != nil || handled {
		return children, handled, err
	}
	children, handled, err = localPortraitParamRefs(raw, path, parent)
	if err != nil || handled {
		return children, handled, err
	}
	children, handled, err = portraitDetailParamRefs(raw, path, parent)
	if err != nil || handled {
		return children, handled, err
	}
	children, handled, err = defectRepairParamRefs(raw, path, parent)
	if err != nil || handled {
		return children, handled, err
	}
	children, handled, err = dustRepairParamRefs(raw, path, parent)
	if err != nil || handled {
		return children, handled, err
	}
	children, handled, err = imageLocalParamRefs(raw, path, parent)
	if err != nil || handled {
		return children, handled, err
	}
	children, handled, err = bodyFitParamRefs(raw, path, parent)
	if err != nil || handled {
		return children, handled, err
	}
	children, handled, err = bodyParamRefs(raw, path, parent)
	if err != nil || handled {
		return children, handled, err
	}
	children, handled, err = statsParamRefs(raw, path, parent)
	if err != nil || handled {
		return children, handled, err
	}
	return manualRasterParamRefs(raw, path, parent)
}

func localPortraitParamRefs(raw []byte, path string, parent v2input.Ref) ([]LayerRef, bool, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, false, v2input.ErrInvalid
	}
	_, hair := fields["detail"]
	_, clothing := fields["wrinkle_strength"]
	_, basePresent := fields["base"]
	_, saturation := fields["saturation_factor"]
	_, hue := fields["hue_delta_degrees"]
	if !((hair || clothing) && (saturation || hue) || basePresent && saturation && hue) {
		return nil, false, nil
	}
	invalid := func() ([]LayerRef, bool, error) { return nil, true, v2input.ErrInvalid }
	if hair == clothing {
		return invalid()
	}
	var base v2input.SessionVersionBinding
	var mask, guard v2input.Ref
	var compiled []byte
	var err error
	if hair {
		var p v2portrait.HairParams
		if json.Unmarshal(raw, &p) != nil {
			return invalid()
		}
		compiled, _, err = p.Compile()
		base, mask, guard = p.Base, p.Mask, p.Protected
	} else {
		var p v2portrait.ClothingParams
		if json.Unmarshal(raw, &p) != nil {
			return invalid()
		}
		compiled, _, err = p.Compile()
		base, mask, guard = p.Base, p.Mask, p.Protected
	}
	actual, e := canonical.Bytes(raw)
	if err != nil || e != nil || !bytes.Equal(actual, compiled) {
		return invalid()
	}
	return []LayerRef{
		{Ref: base.Source, Contract: "WorkingImage", ParentRef: parent, Path: path + ".base.source", CanvasHash: base.CanvasHash},
		{Ref: mask, Contract: "MaskRef", ParentRef: parent, Path: path + ".mask", CanvasHash: base.CanvasHash},
		{Ref: guard, Contract: "MaskRef", ParentRef: parent, Path: path + ".protected", CanvasHash: base.CanvasHash},
	}, true, nil
}
