package interchange

import (
	"context"
	"crypto/ed25519"

	"photoeditor/backend/internal/v2input"
)

// VerifiedImportPersonOrigin is a byte-level receipt selection, not a live
// producer or consent grant. The caller must read receiptRaw from ReceiptFile's
// immutable object version and repeat current authority at activation.
type VerifiedImportPersonOrigin struct {
	Claim       PortablePersonOriginClaim
	ReceiptFile v2input.Ref
	ReceiptSHA  string
}

func VerifyImportedPersonOriginFile(ctx context.Context, parsedJCS, receiptRaw []byte,
	expected PortablePersonOriginExpectation,
	trusted map[PortablePersonOriginKey]ed25519.PublicKey,
) (VerifiedImportPersonOrigin, error) {
	var zero VerifiedImportPersonOrigin
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if _, err := validateParsedImportDocument(ctx, parsedJCS); err != nil {
		return zero, err
	}
	var parsed importParsedDocument
	if err := decodeImportDocument(parsedJCS, &parsed); err != nil {
		return zero, err
	}
	var selected *importBundlePersonOrigin
	for i := range parsed.Manifest.PersonOrigins {
		item := &parsed.Manifest.PersonOrigins[i]
		if item.EntryID == expected.EntryID && item.Recipe == expected.Recipe &&
			item.Override == expected.Override && item.MaskRef == expected.MaskRef &&
			item.MaskPixels == expected.MaskPixels {
			if selected != nil {
				return zero, errPortablePersonOrigin
			}
			selected = item
		}
	}
	if selected == nil || selected.SHA != rawSHA(receiptRaw) {
		return zero, errPortablePersonOrigin
	}
	var staged v2input.Ref
	matches := 0
	for _, file := range parsed.Files {
		if file.Path == selected.Path {
			if file.Contract != "PersonOriginReceipt" || file.Content.SHA256 != selected.SHA || !validImportRef(file.Content) {
				return zero, errPortablePersonOrigin
			}
			staged = file.Content
			matches++
		}
	}
	if matches != 1 {
		return zero, errPortablePersonOrigin
	}
	claim, err := VerifyPortablePersonOrigin(receiptRaw, expected, trusted)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return VerifiedImportPersonOrigin{Claim: claim, ReceiptFile: staged, ReceiptSHA: selected.SHA}, nil
}
