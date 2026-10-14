package workflow

import (
 "bytes"
 "context"
 "encoding/json"
 "io"
 "github.com/google/uuid"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/exportpreset"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// SQLDeliveryWatermarkChildResolver loads current persisted authority, then
// reads exact versioned physical documents/arrays outside SQL transactions.
// Target ICC pins must originate in the trusted deployment catalogue.
type SQLDeliveryWatermarkChildResolver struct{
 DB delivery.MetadataTransactionBeginner
 Objects v2input.SessionAnalysisObjectReader
 Bucket string
 TargetICCs map[string]string
 WorkingICCSHA string
}

func (s SQLDeliveryWatermarkChildResolver) ResolveWatermarkChild(ctx context.Context, l delivery.ExpansionLease, p delivery.PreparePlan, c delivery.ExportChildIntent) (DeliveryResolvedWatermarkChild, error) {
	zero := DeliveryResolvedWatermarkChild{}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Objects == nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 || !l.Valid() || l.ParentTask != p.TaskID() || c.SlotIndex < 0 || c.SlotIndex >= l.SlotCount {
		return zero, v2input.ErrInvalid
	}
	current,scope,err:=delivery.LoadPersistedWatermarkPlan(ctx,s.DB,l)
 if err!=nil{return zero,err}
 configRaw,err:=current.WatermarkChildConfig(l.Run,c);if err!=nil{return zero,err}
 supplied,err:=p.WatermarkChildConfig(l.Run,c);if err!=nil||!bytes.Equal(configRaw,supplied){return zero,v2input.ErrInvalid}
 scope.Preset=c.Preset.PresetID;scope.PresetSHA=c.Preset.PresetSHA
 pins,err:=delivery.NewWatermarkPinStore(s.DB,l.Tenant);if err!=nil{return zero,err}
 pin,err:=pins.Read(ctx,scope,l.Expansion,l.Token,configRaw);if err!=nil{return zero,err}
 request,requestRaw,err:=current.WatermarkExportRequest(l.Run,c,scope,pin)
 if err!=nil{return zero,err}
 _,originalRaw,err:=p.WatermarkExportRequest(l.Run,c,scope,pin)
 if err!=nil||!bytes.Equal(originalRaw,requestRaw){return zero,v2input.ErrInvalid}
 queries:=watermarkChildSourceQueries{db:s.DB,lease:l,slot:c.SlotIndex,request:string(requestRaw)}
 reader:=SQLDeliveryChildResolver{Objects:s.Objects}
 // Scope/config/current plan were derived from persisted SQL authority.
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
		if err := queries.QueryRow(ctx, watermarkChildSourcesQuery, l.Expansion, l.Token, c.SlotIndex, string(requestRaw), after).Scan(&raw); err != nil {
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
		body, err := reader.readJSON(ctx, item, root.max)
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
	stage,err:=pin.CompileStage(scope,configRaw,c.Binding.Source,image);if err!=nil{return zero,err}
 markIndex:=2;if stage.ImageHasAlpha{markIndex=3}
 markRef:=stage.Inputs[markIndex]
 markEntry,ok:=metadata[markRef.ArtifactID]
 if !ok||markEntry.Ref!=markRef||markEntry.Contract!="WorkingImage"||markEntry.MediaType!="application/json"||markEntry.Bytes>1<<20{return zero,v2input.ErrInvalid}
 mark,err:=reader.readJSON(ctx,markEntry,1<<20);if err!=nil{return zero,err}
 rootBodies[markRef.ArtifactID]=mark
 arrays,err:=watermarkSourceDescriptors(image,metadata);if err!=nil{return zero,err}
 markArrays,err:=watermarkSourceDescriptors(mark,metadata);if err!=nil{return zero,err}
 if err=verifyInitialWatermarkSourceArrays(ctx,s.Objects,c.Binding.Source,image,arrays);err!=nil{return zero,err}
 if err=verifyInitialWatermarkSourceArrays(ctx,s.Objects,markRef,mark,markArrays);err!=nil{return zero,err}
 projected, err := projectDeliverySources(ctx, []v2input.Ref{c.Binding.Source, c.Recipe, c.Preset.Config,markRef}, metadata, func(ctx context.Context, item deliverySourceEntry) ([]byte, error) {
		if body, ok := rootBodies[item.Ref.ArtifactID]; ok {
			return body, nil
		}
		return reader.readJSON(ctx, item, 8<<20)
	})
	if err != nil {
		return zero, err
	}
	fresh,err:=pins.Read(ctx,scope,l.Expansion,l.Token,configRaw);if err!=nil{return zero,err}
 if fresh.SHA256()!=pin.SHA256()||!bytes.Equal(fresh.JCS(),pin.JCS()){return zero,v2input.ErrInvalid}
 if err=ctx.Err();err!=nil{return zero,err}
 return DeliveryResolvedWatermarkChild{TargetICCSHA:target,ImageJCS:append([]byte(nil),image...),Entries:projected,Arrays:arrays,Scope:scope,Pin:pin},nil
}

// watermarkSourceDescriptors accepts refs only from the authorized SQL page.
func watermarkSourceDescriptors(raw []byte,metadata map[uuid.UUID]deliverySourceEntry)([]delivery.WatermarkArrayObject,error){
 image,err:=review.ParseWorkingImage(raw);if err!=nil{return nil,err}
 refs:=[]v2input.Ref{{ArtifactID:image.Pixels.ID,SHA256:image.Pixels.SHA256}}
 if image.Alpha!=nil{refs=append(refs,v2input.Ref{ArtifactID:image.Alpha.ID,SHA256:image.Alpha.SHA256})}
 result:=make([]delivery.WatermarkArrayObject,0,len(refs))
 for n,ref:=range refs{
  entry,ok:=metadata[ref.ArtifactID];if !ok||entry.Ref!=ref||entry.Contract!="NpyArray"{return nil,v2input.ErrInvalid}
  contract:="WorkingPixels";if n==1{contract="WorkingAlpha"}
  result=append(result,delivery.WatermarkArrayObject{Contract:contract,Ref:ref,Object:objectstore.Object{Key:entry.Key,VersionID:entry.Version,SHA256:ref.SHA256,ContentType:entry.MediaType,Bytes:entry.Bytes}})
 }
 return result,nil
}
