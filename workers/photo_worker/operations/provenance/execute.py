"""Signer-pool adapter; construction requires an independently qualified operator.

Deliberately not installed by the general CPU operation factory. Successful
staging is still subject to fenced result commit and backend adoption.
"""
from hashlib import sha256
import os
import stat

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.provenance import (
    ProvenanceSignParams, SigningLineage, SigningPolicy, SignedResult,
    decode_provenance,
)
from photo_worker.runtime.artifacts import TaskArtifacts
from photo_worker.runtime.executor import Operation, OperationFailure, OperationResult, TaskCancelled
from photo_worker.runtime.gateway import ArtifactMetadata
from .sdk import sign_staged


def require_signer_profile(profile, policy):
    if (not isinstance(profile, dict) or profile.get('backend') != 'cpu'
            or profile.get('renderer_version') != 'v2.2'
            or profile.get('execution_pool') != 'signer'
            or any(profile.get(name) != getattr(policy, name) for name in (
                'sdk_lock_sha', 'certificate_chain_sha', 'trust_bundle_sha'))):
        raise ValueError('SIGNER_PROFILE_BINDING')
    estimate = profile.get('resource_estimate')
    if not isinstance(estimate, dict) or type(estimate.get('vram_bytes')) is not int or estimate['vram_bytes'] != 0:
        raise ValueError('SIGNER_DEVICE_BINDING')
    # Repeat CompileSigningTask's resource envelope at the execution boundary.
    # Measured installation authority may require larger budgets, never smaller.
    names = ('ram_bytes', 'scratch_bytes', 'output_bytes', 'pixel_count')
    if any(type(estimate.get(name)) is not int or not 0 <= estimate[name] <= 2**53-1 for name in names):
        raise ValueError('SIGNER_RESOURCE_BINDING')
    maximum = profile.get('max_input_json_bytes')
    if (type(maximum) is not int or not 1 << 20 <= maximum <= 128 << 20
            or estimate['ram_bytes'] < maximum or estimate['scratch_bytes'] < 1 << 30
            or estimate['output_bytes'] < (512+8+8) << 20 or estimate['pixel_count'] != 0):
        raise ValueError('SIGNER_RESOURCE_BINDING')


def verified_receipt(receipt, path, check_active=lambda: None):
    """Independently bind bridge output pins to the bytes being uploaded."""
    def guard():
        if check_active() is False:
            raise TaskCancelled('Signer task inactive')
    if not isinstance(receipt, dict):
        raise ValueError('SIGNER_RECEIPT')
    manifest = receipt.get('manifest')
    if not isinstance(manifest, bytes) or not 1 <= len(manifest) <= 8 << 20:
        raise ValueError('SIGNER_MANIFEST_LIMIT')
    if sha256(manifest).hexdigest() != receipt.get('manifest_sha256'):
        raise ValueError('SIGNER_MANIFEST_HASH')
    validation_sha = receipt.get('validation_sha256')
    if (not isinstance(validation_sha, str) or len(validation_sha) != 64
            or any(character not in '0123456789abcdef' for character in validation_sha)):
        raise ValueError('SIGNER_VALIDATION_RECEIPT')
    digest, size = sha256(), 0
    guard()
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        guard()
    except BaseException:
        os.close(fd)
        raise
    with os.fdopen(fd, 'rb') as stream:
        info = os.fstat(stream.fileno())
        guard()
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
            raise ValueError('SIGNER_OUTPUT_FILE')
        identity = (info.st_dev, info.st_ino)
        file_state = (info.st_size, info.st_mtime_ns, info.st_ctime_ns)
        while block := stream.read(1 << 20):
            guard()
            size += len(block)
            if size > 512 << 20:
                raise ValueError('SIGNER_OUTPUT_LIMIT')
            digest.update(block)
        after = os.fstat(stream.fileno())
        if ((after.st_dev, after.st_ino) != identity or
                (after.st_size, after.st_mtime_ns, after.st_ctime_ns) != file_state):
            raise ValueError('SIGNER_OUTPUT_CHANGED')
    if (not size or type(receipt.get('signed_bytes')) is not int
            or size != receipt['signed_bytes']
            or digest.hexdigest() != receipt.get('signed_sha256')):
        raise ValueError('SIGNER_OUTPUT_HASH')
    guard()
    from .sdk import _same_staged_file
    if not _same_staged_file(path, identity):
        raise ValueError('SIGNER_OUTPUT_PATH_CHANGED')
    current = os.stat(path, follow_symlinks=False)
    if (current.st_size, current.st_mtime_ns, current.st_ctime_ns) != file_state:
        raise ValueError('SIGNER_OUTPUT_CHANGED')
    return manifest, size, identity


def execute(ctx, artifacts, operator):
    def guard():
        if ctx.check_active() is False:
            raise TaskCancelled('Signer task inactive')
    guard()
    try:
        params = artifacts.params(ProvenanceSignParams).model_copy(deep=True)
        guard()
        for name, ref in (('QualifiedExport', params.export),
                          ('SigningLineage', params.lineage), ('SigningPolicy', params.policy)):
            declared = artifacts.declared_contract_refs(name, 1)
            guard()
            if declared != (ref,):
                raise ValueError('SIGNER_INPUT_BINDING')
        policy_raw = artifacts.contract_bytes(params.policy.model_copy(deep=True), SigningPolicy)
        guard()
        lineage_raw = artifacts.contract_bytes(params.lineage.model_copy(deep=True), SigningLineage)
        guard()
        policy = decode_provenance(policy_raw, SigningPolicy, 1 << 20)
        decode_provenance(lineage_raw, SigningLineage, 8 << 20)
        if policy.id != params.signing_policy_id:
            raise ValueError('SIGNER_POLICY_BINDING')
        require_signer_profile(ctx.lease.execution_profile, policy)
        metadata = artifacts.input_metadata(params.export.model_copy(deep=True))
        guard()
        if (not isinstance(metadata, ArtifactMetadata) or type(metadata.bytes) is not int
                or not 1 <= metadata.bytes <= 512 << 20
                or metadata.media_type not in policy.allowed_mime):
            raise ValueError('SIGNER_EXPORT_METADATA')
    except ValueError as exc:
        raise OperationFailure('INPUT_INVALID') from exc
    ctx.progress(0, 3)
    guard()
    with artifacts.input(params.export.model_copy(deep=True)) as source:
        guard()
        with source.path.open('rb') as stream:
            magic = stream.read(8)
        mime = 'image/png' if magic == b'\x89PNG\r\n\x1a\n' else 'image/jpeg' if magic[:3] == b'\xff\xd8\xff' else None
        if mime is None or mime != metadata.media_type or source.path.stat().st_size != metadata.bytes:
            raise OperationFailure('INPUT_INVALID')
        with artifacts.scratch.reserve((512 + 8) << 20) as reservation:
            output = reservation.planned_path('signed', '.png' if mime == 'image/png' else '.jpg')
            guard()
            receipt = sign_staged(source.path, output, mime, params.export.sha256,
                                  lineage_raw, policy_raw, operator, check_active=guard)
            guard()
            reservation.validate_file(output)
            manifest, size, identity = verified_receipt(receipt, output, guard)
            current_metadata = artifacts.input_metadata(params.export.model_copy(deep=True))
            guard()
            if current_metadata != metadata:
                raise OperationFailure('INPUT_INVALID')
            guard()
            manifest_path = reservation.path('c2pa', '.bin')
            with artifacts.scratch.open_writer(manifest_path, reservation) as stream:
                stream.write(manifest)
            guard()
            signed_ref = artifacts.stage(output, reservation, 'signed_export', mime,
                                         expected_identity=identity)
            guard()
            if signed_ref.sha256 != receipt['signed_sha256']:
                raise OperationFailure('INPUT_INVALID')
            guard()
            manifest_ref = artifacts.stage(manifest_path, reservation, 'manifest', 'application/c2pa')
            guard()
            if manifest_ref.sha256 != receipt['manifest_sha256']:
                raise OperationFailure('INPUT_INVALID')
            # Uploads can take time; a changed immutable source receipt must
            # not produce a successful SignedResult after publication.
            current_metadata = artifacts.input_metadata(params.export.model_copy(deep=True))
            guard()
            if current_metadata != metadata:
                raise OperationFailure('INPUT_INVALID')
            guard()
    result = SignedResult(schema_version=1, signed_export=signed_ref, manifest=manifest_ref)
    guard()
    root = artifacts.stage_contract(result, 'document')
    guard()
    ctx.progress(3, 3)
    return OperationResult(outputs=[root],
                           bytes_written=size + len(manifest) + len(canonical_bytes(result)))


def signer_operations(config, operator):
    if operator is None:
        return []
    from .installation import InstalledSigningOperator
    if not isinstance(operator, InstalledSigningOperator):
        raise ValueError('SIGNER_INSTALLATION_REQUIRED')
    def run(ctx):
        with TaskArtifacts.create(ctx, config, frozenset({'document', 'signed_export', 'manifest'})) as artifacts:
            return execute(ctx, artifacts, operator)
    return [Operation('provenance.sign', 'cpu', run)]
