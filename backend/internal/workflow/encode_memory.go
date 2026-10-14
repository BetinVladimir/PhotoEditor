package workflow

import "math"

// encodeDimensions matches export.encode._dimensions, including ties-to-even.
func encodeDimensions(w, h int64, size *ExportSize) (int64, int64) {
 if size == nil { return w, h }
 var scale float64
 if size.LongEdge != nil { scale = float64(*size.LongEdge)/float64(max(w,h)) } else {
  scale = math.Min(float64(*size.Width)/float64(w),float64(*size.Height)/float64(h))
 }
 if !size.AllowUpscale { scale = math.Min(1,scale) }
 return max(int64(1),int64(math.RoundToEven(float64(w)*scale))),max(int64(1),int64(math.RoundToEven(float64(h)*scale)))
}

// encodeRAMBytes matches export.memory.presentation_ram_bytes.
func encodeRAMBytes(source, target int64, alpha bool) int64 {
 sourceRAM := int64(24)
 if alpha { sourceRAM = 96 }
 return (128<<20)+source*sourceRAM+target*96
}
