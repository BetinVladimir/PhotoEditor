package interchange

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"time"

	"photoeditor/backend/internal/v2input"
)

// BuildResultInstallerWithAuthority installs the same pinned native probes and
// operator-selected document policy used by export creation. Request-specific
// catalogues and graph geometry are bound later from the canonical result.
func (d NativeExportDeployment) BuildResultInstallerWithAuthority(
	ctx context.Context, objects ImportDocumentObjects, factory ExportNativeAuthorityFactory,
	trusted map[PortablePersonOriginKey]ed25519.PublicKey,
) (SQLExportResultInstaller, error) {
	var zero SQLExportResultInstaller
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if objects == nil || factory == nil {
		return zero, v2input.ErrInvalid
	}
	checked, err := DecodeNativeExportDeployment([]byte(d.raw), rawSHA([]byte(d.raw)))
	if err != nil {
		return zero, err
	}
	var wire nativeExportDeploymentWire
	if err := json.Unmarshal([]byte(checked.raw), &wire); err != nil {
		return zero, v2input.ErrInvalid
	}
	scratch, err := NewExportArchiveScratch(wire.ScratchRoot, wire.ScratchBytes)
	if err != nil {
		return zero, err
	}
	timeout := time.Duration(wire.TimeoutSeconds) * time.Second
	native := ExportNativeResolverInstallation{
		Scratch:         scratch,
		ImageProbe:      PythonExportNativeProbe{Python: wire.Python, WorkerRoot: wire.WorkerRoot, Timeout: timeout},
		MaskProbe:       PythonExportMaskProbe{Python: wire.Python, WorkerRoot: wire.WorkerRoot, Timeout: timeout},
		ArrayProbe:      PythonExportArrayProbe{Python: wire.Python, WorkerRoot: wire.WorkerRoot, Timeout: timeout},
		ImageRuntimeSHA: wire.ImageRuntime, ArrayRuntimeSHA: wire.ArrayRuntime,
	}
	validator, err := factory.BuildNativeExportAuthority(ctx, native)
	if err != nil {
		return zero, err
	}
	if validator == nil {
		return zero, v2input.ErrInvalid
	}
	ownedTrust := make(map[PortablePersonOriginKey]ed25519.PublicKey, len(trusted))
	for key, public := range trusted {
		ownedTrust[key] = append(ed25519.PublicKey(nil), public...)
	}
	return SQLExportResultInstaller{
		Objects: objects, Scratch: scratch, ValidatorFactory: validator,
		Native: &native, MaxInputBytes: wire.MaxInputBytes,
		PersonOriginTrusted: ownedTrust,
	}, ctx.Err()
}
