package interchange

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path"
	"path/filepath"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"strings"
	"sync"
)

// Share one instance per scratch directory/process. This is a process budget;
// deployment must dedicate the directory and enforce a filesystem disk quota.
type ExportArchiveScratch struct {
	root          string
	maximum, used int64
	mu            sync.Mutex
}

func NewExportArchiveScratch(root string, maximum int64) (*ExportArchiveScratch, error) {
	if !filepath.IsAbs(root) || maximum < 23 || maximum > 128<<30 {
		return nil, v2input.ErrInvalid
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, v2input.ErrInvalid
	}
	return &ExportArchiveScratch{root: root, maximum: maximum}, nil
}
func (s *ExportArchiveScratch) reserve(bytes int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if bytes < 1 || bytes > s.maximum-s.used {
		return false
	}
	s.used += bytes
	return true
}
func (s *ExportArchiveScratch) release(bytes int64) { s.mu.Lock(); s.used -= bytes; s.mu.Unlock() }

type exportArchiveTeeObjects struct {
	source   ImportDocumentObjects
	target   io.Writer
	expected objectstore.Object
}
type exportArchiveTeeStream struct {
	io.Reader
	closer io.Closer
}

func (s exportArchiveTeeStream) Close() error { return s.closer.Close() }
func (s exportArchiveTeeObjects) Open(ctx context.Context, object objectstore.Object) (io.ReadCloser, error) {
	if object != s.expected {
		return nil, v2input.ErrInvalid
	}
	stream, err := s.source.Open(ctx, object)
	if err != nil {
		return nil, err
	}
	if stream == nil {
		return nil, v2input.ErrInvalid
	}
	return exportArchiveTeeStream{Reader: io.TeeReader(stream, s.target), closer: stream}, nil
}

type VerifiedExportArchive struct {
	bundle        VerifiedExportBundleBytes
	files         []VerifiedExportZIPFile
	manifest      []byte
	personOrigins []PortablePersonOriginClaim
}

func (v VerifiedExportArchive) Bundle() v2input.Ref { return v.bundle.Ref() }
func (v VerifiedExportArchive) Files() []VerifiedExportZIPFile {
	return append([]VerifiedExportZIPFile(nil), v.files...)
}
func (v VerifiedExportArchive) Manifest() []byte { return append([]byte(nil), v.manifest...) }
func (v VerifiedExportArchive) PersonOrigins() []PortablePersonOriginClaim {
	return append([]PortablePersonOriginClaim(nil), v.personOrigins...)
}

type ExportArchiveReader struct {
	Catalog       ExportOutputCatalog
	Objects       ImportDocumentObjects
	Scratch       *ExportArchiveScratch
	PersonOrigins *ExportPersonOriginArchiveAuthority
}

// Read joins physical SHA, bounded staging, ZIP CRC/inventory and manifest frozen
// binding. XMP/source/native dependency semantics and SQL adoption are separate
// mandatory gates. No member path is extracted to a filesystem destination.
func (r ExportArchiveReader) Read(ctx context.Context, intent CreateIntent, frozen FrozenExportDocuments, ref v2input.Ref) (result VerifiedExportArchive, err error) {
	if err = ctx.Err(); err != nil {
		return
	}
	if r.Catalog == nil || r.Objects == nil || r.Scratch == nil || !validImportRef(ref) {
		err = v2input.ErrInvalid
		return
	}
	_, compiled, decodeErr := DecodeFrozenExportDocuments(ctx, intent, frozen.Body())
	if decodeErr != nil {
		err = decodeErr
		return
	}
	var plan importPlanDocument
	if err = decodeImportDocument(compiled.planBody, &plan); err != nil {
		return
	}
	entry := v2input.Entry{Ref: ref, Contract: "InterchangeArchive"}
	object, lookupErr := r.Catalog.LookupExportOutput(ctx, entry)
	if lookupErr != nil {
		err = lookupErr
		return
	}
	if object.Bytes < 22 || object.Bytes > plan.Archive || !r.Scratch.reserve(object.Bytes+1) {
		err = v2input.ErrInvalid
		return
	}
	file, createErr := os.CreateTemp(r.Scratch.root, "export-archive-*")
	if createErr != nil {
		r.Scratch.release(object.Bytes + 1)
		err = createErr
		return
	}
	defer func() {
		closeErr := file.Close()
		removeErr := os.Remove(file.Name())
		// Retain the reservation if cleanup failed: untracked leftovers must not
		// silently increase available scratch capacity.
		if removeErr == nil {
			r.Scratch.release(object.Bytes + 1)
		}
		if err == nil {
			if closeErr != nil {
				err = closeErr
			} else if removeErr != nil {
				err = removeErr
			}
		}
		if err != nil {
			result = VerifiedExportArchive{}
		}
	}()
	physical, readErr := (ExportBundleReader{Catalog: r.Catalog, Objects: exportArchiveTeeObjects{r.Objects, file, object}}).Read(ctx, intent, frozen, ref)
	if readErr != nil {
		err = readErr
		return
	}
	limits := ExportLimits{ArchiveBytes: plan.Archive, ExpandedBytes: plan.Expanded, Files: plan.Files}
	files, verifyErr := VerifyExportZIPInventory(ctx, file, physical.Bytes(), limits)
	if verifyErr != nil {
		err = verifyErr
		return
	}
	archive, zipErr := zip.NewReader(file, physical.Bytes())
	if zipErr != nil {
		err = zipErr
		return
	}
	var manifest []byte
	for _, member := range archive.File {
		if member.Name == "manifest.json" {
			if member.UncompressedSize64 > 8<<20 {
				err = v2input.ErrInvalid
				return
			}
			stream, openErr := member.Open()
			if openErr != nil {
				err = openErr
				return
			}
			manifest, err = io.ReadAll(io.LimitReader(exportContextReader{ctx, stream}, (8<<20)+1))
			closeErr := stream.Close()
			if err != nil {
				return
			}
			if closeErr != nil {
				err = closeErr
				return
			}
			break
		}
	}
	var originClaims []PortablePersonOriginClaim
	if r.PersonOrigins == nil {
		err = ValidateExportManifest(ctx, intent, frozen, manifest, files)
	} else {
		err = validateExportManifest(ctx, intent, frozen, manifest, files, true)
		if err == nil {
			err = r.PersonOrigins.verifyPinnedReceipts(files)
		}
		if err == nil {
			originClaims, err = verifyExportPersonOriginArchiveClaims(ctx, archive, manifest, files,
				r.PersonOrigins.expected, r.PersonOrigins.trusted, r.PersonOrigins.current)
		}
	}
	if err != nil {
		return
	}
	if plan.Format != "photoeditor_bundle" {
		members := map[string]*zip.File{}
		for _, member := range archive.File {
			members[member.Name] = member
		}
		for _, item := range plan.Entries {
			member := members[strings.TrimSuffix(item.Path, path.Ext(item.Path))+".xmp"]
			if member == nil || member.UncompressedSize64 > 1<<20 {
				err = v2input.ErrInvalid
				return
			}
			stream, openErr := member.Open()
			if openErr != nil {
				err = openErr
				return
			}
			payload, readErr := io.ReadAll(io.LimitReader(exportContextReader{ctx, stream}, (1<<20)+1))
			closeErr := stream.Close()
			if readErr != nil {
				err = readErr
				return
			}
			if closeErr != nil {
				err = closeErr
				return
			}
			orientation, validateErr := validateExportXMP(ctx, payload, item)
			if validateErr != nil {
				err = validateErr
				return
			}
			image := members[item.Path]
			if image == nil || image.Method != zip.Store {
				err = v2input.ErrInvalid
				return
			}
			offset, offsetErr := image.DataOffset()
			if offsetErr != nil {
				err = offsetErr
				return
			}
			observed, metadataErr := exportEncodedOrientation(ctx, io.NewSectionReader(file, offset, int64(image.UncompressedSize64)), int64(image.UncompressedSize64), item.Path)
			if metadataErr != nil {
				err = metadataErr
				return
			}
			if observed != orientation {
				err = v2input.ErrInvalid
				return
			}
		}
	}
	current, lookupErr := r.Catalog.LookupExportOutput(ctx, entry)
	if lookupErr != nil {
		err = lookupErr
		return
	}
	if current != object {
		err = v2input.ErrInvalid
		return
	}
	result = VerifiedExportArchive{bundle: physical, files: files, manifest: append([]byte(nil), manifest...),
		personOrigins: append([]PortablePersonOriginClaim(nil), originClaims...)}
	err = ctx.Err()
	return
}
