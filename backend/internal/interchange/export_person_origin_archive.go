package interchange

import (
	"archive/zip"
	"context"
	"crypto/ed25519"
	"io"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// ExportPersonOriginArchiveAuthority owns process-provisioned trust and
// source-derived expectations for one frozen export. The constructor copies
// all caller buffers so callbacks cannot mutate a running archive check.
type ExportPersonOriginArchiveAuthority struct {
	expected map[string]PortablePersonOriginExpectation
	trusted  map[PortablePersonOriginKey]ed25519.PublicKey
}

func NewExportPersonOriginArchiveAuthority(expected map[string]PortablePersonOriginExpectation,
	trusted map[PortablePersonOriginKey]ed25519.PublicKey,
) (*ExportPersonOriginArchiveAuthority, error) {
	if expected == nil || trusted == nil || len(expected) > 5000 {
		return nil, v2input.ErrInvalid
	}
	owned := &ExportPersonOriginArchiveAuthority{
		expected: make(map[string]PortablePersonOriginExpectation, len(expected)),
		trusted:  make(map[PortablePersonOriginKey]ed25519.PublicKey, len(trusted)),
	}
	for path, scope := range expected {
		if !validImportPlanPath(path) || scope.EntryID == uuid.Nil ||
			!validImportRef(scope.Recipe) || !validImportRef(scope.Override) ||
			!validImportRef(scope.MaskRef) || !validImportRef(scope.MaskPixels) ||
			!portablePersonOriginSHA.MatchString(scope.ModelLockSHA) {
			return nil, v2input.ErrInvalid
		}
		owned.expected[path] = scope
	}
	for key, public := range trusted {
		if key.IssuerID == uuid.Nil || !portablePersonOriginSHA.MatchString(key.KeyID) ||
			len(public) != ed25519.PublicKeySize {
			return nil, v2input.ErrInvalid
		}
		owned.trusted[key] = append(ed25519.PublicKey(nil), public...)
	}
	return owned, nil
}

// VerifyExportPersonOriginArchive reads the actual receipt members after the
// complete ZIP inventory has been physically hashed. The caller supplies
// source-derived expectations and process-provisioned issuer keys; neither is
// taken from the archive manifest.
func VerifyExportPersonOriginArchive(ctx context.Context, archive *zip.Reader, raw []byte,
	verified []VerifiedExportZIPFile, expected map[string]PortablePersonOriginExpectation,
	trusted map[PortablePersonOriginKey]ed25519.PublicKey,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if archive == nil || expected == nil || trusted == nil {
		return v2input.ErrInvalid
	}
	var manifest exportBundleManifest
	if err := decodeImportDocument(raw, &manifest); err != nil {
		return err
	}
	if len(manifest.PersonOrigins) != len(expected) || len(expected) > 5000 {
		return v2input.ErrInvalid
	}
	inventory := make(map[string]exportBundleInventoryEntry, len(manifest.Inventory))
	for _, item := range manifest.Inventory {
		if _, duplicate := inventory[item.Path]; duplicate {
			return v2input.ErrInvalid
		}
		inventory[item.Path] = item
	}
	physical := make(map[string]VerifiedExportZIPFile, len(verified))
	for _, file := range verified {
		if _, duplicate := physical[file.Path]; duplicate {
			return v2input.ErrInvalid
		}
		physical[file.Path] = file
	}
	members := make(map[string]*zip.File, len(archive.File))
	for _, file := range archive.File {
		if _, duplicate := members[file.Name]; duplicate {
			return v2input.ErrInvalid
		}
		members[file.Name] = file
	}
	seen := map[string]bool{}
	for _, origin := range manifest.PersonOrigins {
		if err := ctx.Err(); err != nil {
			return err
		}
		scope, ok := expected[origin.Path]
		file := members[origin.Path]
		actual, present := physical[origin.Path]
		entry, inventoried := inventory[origin.Path]
		path := "origins/" + scope.EntryID.String() + "/" + scope.MaskRef.ArtifactID.String() + ".json"
		identity := uuid.NewSHA1(scope.EntryID, []byte("v30:person-origin:"+scope.MaskRef.ArtifactID.String()))
		if !ok || seen[origin.Path] || file == nil || !present ||
			!inventoried || origin.Path != path || entry.Contract != "PersonOriginReceipt" ||
			entry.ID != identity || entry.SHA != origin.SHA ||
			file.UncompressedSize64 > 16384 || actual.Bytes != int64(file.UncompressedSize64) ||
			actual.SHA256 != origin.SHA || scope.EntryID != origin.EntryID ||
			scope.Recipe != origin.Recipe || scope.Override != origin.Override ||
			scope.MaskRef != origin.MaskRef || scope.MaskPixels != origin.MaskPixels {
			return v2input.ErrInvalid
		}
		stream, err := file.Open()
		if err != nil {
			return err
		}
		body, readErr := io.ReadAll(io.LimitReader(exportContextReader{ctx, stream}, 16385))
		closeErr := stream.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		if int64(len(body)) != actual.Bytes || rawSHA(body) != actual.SHA256 {
			return v2input.ErrInvalid
		}
		if _, err := VerifyPortablePersonOrigin(body, scope, trusted); err != nil {
			return err
		}
		seen[origin.Path] = true
	}
	return ctx.Err()
}
