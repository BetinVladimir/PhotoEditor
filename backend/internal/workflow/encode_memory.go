package workflow

import "photoeditor/backend/internal/encodecontract"

func encodeDimensions(w, h int64, size *ExportSize) (int64, int64) {
	return encodecontract.EncodeDimensions(w, h, size)
}
func encodeRAMBytes(source, target int64, alpha bool) int64 {
	return encodecontract.EncodeRAMBytes(source, target, alpha)
}
