package workflow

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/masknpy"
 "photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// VerifyDeliveryStageOutput independently reads pinned JSON and streams binary
// arrays through the shared NPY verifier. This verifies bytes/shape/finiteness,
// not photographic equivalence or current SQL authority at adoption commit.
func VerifyDeliveryStageOutput(ctx context.Context, objects v2input.SessionAnalysisObjectReader, bucket string, node CompiledDownloadNode, source []byte, result DeliveryStageResult) (DeliveryPresentationOutput, error) {
 if err:=ctx.Err();err!=nil{return DeliveryPresentationOutput{},err}
 if node.Operation!="derivative.resize"&&node.Operation!="derivative.sharpen"{return DeliveryPresentationOutput{},v2input.ErrInvalid}
 return verifyDeliveryStageOutput(ctx,objects,bucket,node,result,func(imageRef review.ArtifactRef,imageRaw []byte,reportRef review.ArtifactRef,reportRaw []byte)(review.WorkingImage,error){
  if node.Operation=="derivative.resize"{return ValidateDeliveryResizeResult(node,source,imageRef,imageRaw,reportRef,reportRaw)}
  return ValidateDeliverySharpenResult(node,source,imageRef,imageRaw,reportRef,reportRaw)
 })
}

// VerifyDeliveryWatermarkOutput uses the shared streaming NPY verifier after
// pinned value validation. It is not a SQL producer/adoption receipt.
func VerifyDeliveryWatermarkOutput(ctx context.Context,objects v2input.SessionAnalysisObjectReader,bucket string,node CompiledDownloadNode,pin delivery.FrozenWatermarkSource,scope delivery.WatermarkPinScope,configRaw,source []byte,result DeliveryStageResult)(DeliveryPresentationOutput,error){
 if err:=ctx.Err();err!=nil{return DeliveryPresentationOutput{},err}
 owned,err:=delivery.DecodeWatermarkSourcePin(pin.JCS(),pin.SHA256(),scope,configRaw);if err!=nil{return DeliveryPresentationOutput{},err}
 config:=append([]byte(nil),configRaw...);source=append([]byte(nil),source...)
 return verifyDeliveryStageOutput(ctx,objects,bucket,node,result,func(imageRef review.ArtifactRef,imageRaw []byte,reportRef review.ArtifactRef,reportRaw []byte)(review.WorkingImage,error){
  return ValidateDeliveryWatermarkResult(node,owned,scope,config,source,imageRef,imageRaw,reportRef,reportRaw)
 })
}

func verifyDeliveryStageOutput(ctx context.Context,objects v2input.SessionAnalysisObjectReader,bucket string,node CompiledDownloadNode,result DeliveryStageResult,validate func(review.ArtifactRef,[]byte,review.ArtifactRef,[]byte)(review.WorkingImage,error))(DeliveryPresentationOutput,error){
 zero:=DeliveryPresentationOutput{}
 if err:=ctx.Err();err!=nil{return zero,err}
 if objects==nil||validate==nil{return zero,v2input.ErrInvalid}
	raw, err := json.Marshal(result)
	if err != nil {
		return zero, err
	}
	checked, err := parseDeliveryStageResult(raw, node.ID, bucket)
	if err != nil {
		return zero, err
	}
	roles := map[string]DeliveryStageOutput{}
	for _, output := range checked.Outputs {
		roles[output.Role] = output
	}
	read := func(role string) ([]byte, error) {
		output := roles[role]
		return (SQLDeliveryChildResolver{Objects: objects}).readJSON(ctx, deliverySourceEntry{Ref: output.Ref, Key: output.Key, Version: output.Version, Bytes: output.Bytes, MediaType: output.MediaType}, 1<<20)
	}
	imageRaw, err := read("working_image")
	if err != nil {
		return zero, err
	}
	reportRaw, err := read("derivative_report")
	if err != nil {
		return zero, err
	}
	imageRef := review.ArtifactRef{ID: roles["working_image"].Ref.ArtifactID, SHA256: roles["working_image"].Ref.SHA256}
	reportRef := review.ArtifactRef{ID: roles["derivative_report"].Ref.ArtifactID, SHA256: roles["derivative_report"].Ref.SHA256}
	image, err := validate(imageRef,imageRaw,reportRef,reportRaw)
	if err != nil {
		return zero, err
	}
 if image.Canvas.Width<1||image.Canvas.Height<1||image.Canvas.Width>100000||image.Canvas.Height>100000||image.Canvas.Width*image.Canvas.Height>200000000{return zero,v2input.ErrInvalid}
	pixels := roles["working_rgb"]
	if pixels.Ref.ArtifactID != image.Pixels.ID || pixels.Ref.SHA256 != image.Pixels.SHA256 {
		return zero, v2input.ErrInvalid
	}
	alpha, hasAlpha := roles["working_alpha"]
	if hasAlpha != (image.Alpha != nil) || (hasAlpha && (alpha.Ref.ArtifactID != image.Alpha.ID || alpha.Ref.SHA256 != image.Alpha.SHA256)) {
		return zero, v2input.ErrInvalid
	}
	for _, output := range checked.Outputs {
		if output.Role != "working_rgb" && output.Role != "working_alpha" {
			continue
		}
		width, height := image.Canvas.Width, image.Canvas.Height
		max := width*height*12 + 8192
		if output.Role == "working_alpha" {
			max = width*height*4 + 8192
		}
		if output.Bytes > max {
			return zero, v2input.ErrInvalid
		}
		reader, err := objects.Open(ctx, objectstore.Object{Key: output.Key, VersionID: output.Version, SHA256: output.Ref.SHA256, ContentType: output.MediaType, Bytes: output.Bytes})
		if err != nil {
			return zero, physicalDeliveryFailure(output.Ref,output.Version,err)
		}
		if reader == nil {
			return zero, v2input.ErrInvalid
		}
		if output.Role == "working_rgb" {
			err = masknpy.VerifyFloat32ArrayNPY(ctx, reader, output.Bytes, output.Ref.SHA256, width, height, 3, false)
		} else {
			err = masknpy.VerifyAlphaNPY(ctx, reader, output.Bytes, output.Ref.SHA256, width, height)
		}
		closeErr := reader.Close()
		if err != nil {
			return zero, physicalDeliveryFailure(output.Ref,output.Version,err)
		}
		if closeErr != nil {
			return zero, physicalDeliveryFailure(output.Ref,output.Version,closeErr)
		}
	}
 if err=ctx.Err();err!=nil{return zero,err}
	return DeliveryPresentationOutput{ImageRef: imageRef, ImageJSON: imageRaw, ReportRef: reportRef, ReportJSON: reportRaw}, nil
}
