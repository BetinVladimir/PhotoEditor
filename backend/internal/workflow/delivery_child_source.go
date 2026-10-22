package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/exportpreset"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type deliverySourceEntry struct {
	Ordinal   int         `json:"ordinal"`
	Ref       v2input.Ref `json:"ref"`
	Contract  string      `json:"contract"`
	Bucket    string      `json:"bucket"`
	Key       string      `json:"object_key"`
	Version   string      `json:"object_version"`
	Bytes     int64       `json:"bytes"`
	MediaType string      `json:"media_type"`
}

// SQLDeliveryChildResolver inherits the adopted prepare input inventory. SQL
// repeats current project/session/source/lease authority on every bounded page.
// Physical JSON roots and exact declared RGB/alpha arrays are verified here;
// workers independently verify the same immutable inventory.
// TargetICCs must come from the scheduler's verified deployment ICC catalogue.
type SQLDeliveryChildResolver struct {
	DB            artifactjson.RowQuerier
	Objects       v2input.SessionAnalysisObjectReader
	Bucket        string
	TargetICCs    map[string]string
	WorkingICCSHA string
}

func (s SQLDeliveryChildResolver) ResolveChild(ctx context.Context, l delivery.ExpansionLease, p delivery.PreparePlan, c delivery.ExportChildIntent) (result DeliveryResolvedChild, failure error) {
	zero := DeliveryResolvedChild{}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Objects == nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 || l.ParentTask != p.TaskID() {
		return zero, v2input.ErrInvalid
	}
	request, requestRaw, err := p.ExportRequest(l.Run, c)
	if err != nil {
		return zero, err
	}
	defer func(){bindDeliveryPhysicalFailure(&failure,c.SlotIndex,string(requestRaw),false)}()
	config, err := exportpreset.DecodeFrozenConfig([]byte(request.ConfigJCS), request.Preset.PresetSHA)
	if err != nil {
		return zero, err
	}
	target := s.TargetICCs[config.ICCID]
	if len(target) != 64 {
		return zero, v2input.ErrInvalid
	}
	for _, ch := range target {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return zero, v2input.ErrInvalid
		}
	}
	entries := make([]v2input.Entry, 0)
	metadata := make(map[uuid.UUID]deliverySourceEntry)
	after := -1
	for {
		var raw []byte
		if err := s.DB.QueryRow(ctx, `SELECT photo.v30_delivery_child_sources($1,$2,$3,$4,$5)`, l.Expansion, l.Token, c.SlotIndex, string(requestRaw), after).Scan(&raw); err != nil {
			return zero, err
		}
		if len(raw) < 2 || len(raw) > 2<<20 {
			return zero, v2input.ErrInvalid
		}
		var page struct {
			Items []deliverySourceEntry `json:"items"`
			Next  *int                  `json:"next"`
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&page) != nil || decoder.Decode(new(any)) != io.EOF || len(page.Items) > 1000 {
			return zero, v2input.ErrInvalid
		}
		// NULL and malformed envelopes never mean an empty authorized inventory.
		var shape map[string]json.RawMessage
		if json.Unmarshal(raw, &shape) != nil || len(shape) != 2 || shape["items"] == nil || shape["next"] == nil || bytes.Equal(bytes.TrimSpace(shape["items"]), []byte("null")) {
			return zero, v2input.ErrInvalid
		}
		if len(page.Items) == 0 {
			if page.Next != nil {
				return zero, v2input.ErrInvalid
			}
			break
		}
		for _, item := range page.Items {
			if item.Ordinal <= after || item.Ordinal > 1000000 || item.Ref.ArtifactID == uuid.Nil || len(item.Ref.SHA256) != 64 || item.Contract == "" || len(item.Contract) > 256 || item.Bucket != s.Bucket || item.Key == "" || item.Version == "" || item.Version == "null" || item.Bytes < 1 || item.MediaType == "" {
				return zero, v2input.ErrInvalid
			}
			if _, exists := metadata[item.Ref.ArtifactID]; exists {
				return zero, v2input.ErrInvalid
			}
			metadata[item.Ref.ArtifactID] = item
			entries = append(entries, v2input.Entry{Ref: item.Ref, Contract: item.Contract})
			after = item.Ordinal
			if len(entries) > 1000000 {
				return zero, v2input.ErrInvalid
			}
		}
		if page.Next == nil || *page.Next != after {
			return zero, v2input.ErrInvalid
		}
	}
	var image []byte
	rootBodies := make(map[uuid.UUID][]byte)
	for _, root := range []struct {
		ref      v2input.Ref
		contract string
		max      int64
	}{{c.Binding.Source, "WorkingImage", 1 << 20}, {c.Recipe, "EditingRecipeV3", 4 << 20}, {c.Preset.Config, "ExportPresetConfig", 1 << 20}} {
		item, exists := metadata[root.ref.ArtifactID]
		if !exists || item.Ref != root.ref || item.Contract != root.contract || item.MediaType != "application/json" || item.Bytes > root.max {
			return zero, v2input.ErrInvalid
		}
		body, err := s.readJSON(ctx, item, root.max)
		if err != nil {
			return zero, err
		}
		rootBodies[item.Ref.ArtifactID] = body
		if root.contract == "WorkingImage" {
			image = body
		}
		if root.contract == "ExportPresetConfig" && !bytes.Equal(body, []byte(request.ConfigJCS)) {
			return zero, v2input.ErrInvalid
		}
	}
	if err := validateDeliverySourceContracts(image, rootBodies[c.Recipe.ArtifactID], c.Binding, s.WorkingICCSHA); err != nil {
		return zero, err
	}
	arrays,arrayErr:=watermarkSourceDescriptors(image,metadata);if arrayErr!=nil{return zero,arrayErr}
	if arrayErr=verifyExportWorkingImageArrays(ctx,s.Objects,c.Binding.Source,image,arrays);arrayErr!=nil{return zero,arrayErr}
	projected, err := projectDeliverySources(ctx, []v2input.Ref{c.Binding.Source, c.Recipe, c.Preset.Config}, metadata, func(ctx context.Context, item deliverySourceEntry) ([]byte, error) {
		if body, ok := rootBodies[item.Ref.ArtifactID]; ok {
			return body, nil
		}
		return s.readJSON(ctx, item, 8<<20)
	})
	if err != nil {
		return zero, err
	}
	return DeliveryResolvedChild{ImageJCS: image, TargetICCSHA: target, Entries: projected}, nil
}
func (s SQLDeliveryChildResolver) readJSON(ctx context.Context, item deliverySourceEntry, max int64) ([]byte, error) {
	object := objectstore.Object{Key: item.Key, VersionID: item.Version, SHA256: item.Ref.SHA256, ContentType: item.MediaType, Bytes: item.Bytes}
	reader, err := s.Objects.Open(ctx, object)
	if err != nil {
		return nil, physicalDeliveryFailure(item.Ref,item.Version,err)
	}
	if reader == nil {
		return nil, v2input.ErrInvalid
	}
	body, err := io.ReadAll(io.LimitReader(reader, max+1))
 closeErr:=reader.Close()
	if err != nil {
		return nil, physicalDeliveryFailure(item.Ref,item.Version,err)
	}
	if closeErr!=nil{return nil,physicalDeliveryFailure(item.Ref,item.Version,closeErr)}
	if int64(len(body)) != item.Bytes || int64(len(body)) > max || renderDerivativeBytesSHA(body) != item.Ref.SHA256 {
		return nil, physicalDeliveryFailure(item.Ref,item.Version,objectstore.ErrIntegrity)
	}
	exact, err := canonical.Marshal(json.RawMessage(body))
	if err != nil || !bytes.Equal(exact, body) {
		return nil, physicalDeliveryFailure(item.Ref,item.Version,objectstore.ErrIntegrity)
	}
	return body, nil
}
