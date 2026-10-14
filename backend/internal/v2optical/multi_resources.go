package v2optical

import (
	"math/bits"
	"photoeditor/backend/internal/v2input"
)

type MultiResourceEstimate PerspectiveResourceEstimate

// MultiResources reserves a conservative upper bound before ORB determines the
// panorama canvas. max_pixels is a real reservation bound, not a target size.
// Includes original downloads, linear copies, projections, registration pages,
// blend/pyramids, encoded outputs and bounded metadata allowance; VRAM is zero.
func MultiResources(operation string, paramsRaw []byte, originals [][]byte) (MultiResourceEstimate, error) {
	images, err := VerifyMultiWorkingImages(operation, paramsRaw, originals)
	if err != nil {
		return MultiResourceEstimate{}, err
	}
	n := int64(len(images))
	var reference int
	var output, width int64
	var cylindrical bool
	if operation == "optical.panorama" {
		p, _ := ParsePanoramaParams(paramsRaw)
		reference = p.ReferenceOrdinal
		output = int64(p.MaxPixels)
		width = output
		cylindrical = p.Projection == "cylindrical"
	} else {
		p, _ := ParseFocusStackParams(paramsRaw)
		reference = p.ReferenceOrdinal
		output = images[reference].Canvas.Width * images[reference].Canvas.Height
		width = images[reference].Canvas.Width
		if output > int64(p.MaxPixels) {
			return MultiResourceEstimate{}, v2input.ErrInvalid
		}
	}
	const head = int64(256 * 1024 * 1024)
	const tile = int64(256)
	const header = int64(4096)
	var sourcePixels, downloads, maxSource, maxWidth int64
	for _, image := range images {
		pixels := image.Canvas.Width * image.Canvas.Height
		sourcePixels += pixels
		maxSource = max(maxSource, pixels)
		maxWidth = max(maxWidth, image.Canvas.Width)
	}
	specs, err := MultiSourceArrays(operation, paramsRaw, originals)
	if err != nil {
		return MultiResourceEstimate{}, err
	}
	for _, spec := range specs {
		channels := int64(spec.Array.Channels)
		if spec.Array.Alpha {
			channels = 1
		}
		downloads += 4*channels*spec.Array.Width*spec.Array.Height + 65536
	}
	sourceDisk := 16*sourcePixels + 2*n*header
	sourceRAM := 16*sourcePixels + sourceDisk + 128*tile*tile + head
	proxyRAM := 16*sourcePixels + (4*n+16)*2048*2048 + head
	registrationDisk := n * (25*output + 4*header)
	var stageDisk, stageRAM int64
	if operation == "optical.panorama" {
		stageDisk = 17*output + 3*header
		stageRAM = 17*n*output + stageDisk + 256*(tile+128)*(tile+128) + 16*width + head
	} else {
		h, w := images[reference].Canvas.Height, width
		levels := max(1, min(6, bits.Len64(uint64(min(h, w)))-1-4))
		sizes := []int64{h * w}
		for i := 0; i < levels; i++ {
			h = (h + 1) / 2
			w = (w + 1) / 2
			sizes = append(sizes, h*w)
		}
		var sum, coarse, fine int64
		for i, pixels := range sizes {
			sum += pixels
			if i > 0 {
				coarse += pixels
			}
			if i < len(sizes)-1 {
				fine += pixels
			}
		}
		length := int64(len(sizes))
		arrays := n*(3*length-2) + 3*length + 2
		stageDisk = n*(24*coarse+8*sum+24*fine) + 24*fine + 48*sum + 17*output + arrays*header
		stageRAM = 17*n*output + stageDisk + 256*(2*tile+10)*(2*tile+10) + 16*width + head
	}
	planarRAM := 16*sourcePixels + registrationDisk + stageRAM + 256*tile*tile + head
	var projectionDisk, projectionRAM int64
	if cylindrical {
		projectionDisk = 25*sourcePixels + 4*n*header
		projectionRAM = 16*sourcePixels + projectionDisk + 16*maxSource + 25*maxSource + 4*header + 256*tile*tile + head + 4096*maxWidth
	}
	referencePixels := images[reference].Canvas.Width * images[reference].Canvas.Height
	encodedDisk := 20*output + 3*header
	encodingRAM := 16*referencePixels + 16*output + encodedDisk + 128*tile*tile + head
	metadata := int64(64 * 1024 * 1024)
	required := MultiResourceEstimate{PixelCount: output, VRAMBytes: 0,
		RAMBytes:     downloads + sourceRAM + proxyRAM + projectionRAM + planarRAM + encodingRAM + metadata,
		ScratchBytes: downloads + sourceDisk + projectionDisk + registrationDisk + stageDisk + encodedDisk + metadata,
		OutputBytes:  encodedDisk + projectionDisk + metadata}
	for _, value := range []int64{required.RAMBytes, required.ScratchBytes, required.OutputBytes} {
		if value < 1 || value > 1<<53-1 {
			return MultiResourceEstimate{}, v2input.ErrInvalid
		}
	}
	return required, nil
}
func ValidateMultiCapacity(required, capacity MultiResourceEstimate) error {
	return ValidatePerspectiveCapacity(PerspectiveResourceEstimate(required), PerspectiveResourceEstimate(capacity))
}
