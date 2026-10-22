"""Independent verification of scheduler-issued local historical producer receipts.

Trusted public keys are process configuration. Archive keys, unsigned ancestry,
and foreign producer assertions never enter this local canonical authority.
"""
import base64
from hashlib import sha256
from typing import Literal
from uuid import UUID, uuid5
from pydantic import Field
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
from photo_worker.contracts.base import Contract, ArtifactRef, canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.session.contracts import VersionBinding
from .adoption import AdoptionInvalid

_DOMAIN = b'photoeditor:historical-origin:v1\x00'


class HistoricalOriginPayload(Contract):
    schema_tag: Literal['historical-origin-payload/v1'] = Field(alias='schema')
    tenant_id: UUID
    project_id: UUID
    workflow_id: UUID
    run_id: UUID
    entry_id: UUID
    target: VersionBinding
    original_source: ArtifactRef
    contract: Literal['WorkingImage', 'FaceBoxes', 'FaceLandmarks']
    original: ArtifactRef
    prepared: ArtifactRef
    original_image: ArtifactRef
    original_boxes: ArtifactRef | None
    model_lock_id: str
    backend: str
    producer_mode: Literal['local_canonical']


class HistoricalOriginReceipt(Contract):
    schema_tag: Literal['historical-origin-receipt/v1'] = Field(alias='schema')
    issuer_id: UUID
    key_id: str
    payload: str
    signature: str


def verify_historical_origin_receipt(raw, trusted):
    if not isinstance(raw, bytes) or not 2 <= len(raw) <= 65536:
        raise AdoptionInvalid('INTERCHANGE_HISTORICAL_RECEIPT')
    try:
        receipt = HistoricalOriginReceipt.model_validate_json(raw)
        if canonical_bytes(receipt) != raw or not 1 <= len(receipt.key_id) <= 120:
            raise ValueError('canonical receipt')
        public = trusted.get((receipt.issuer_id, receipt.key_id))
        if not isinstance(public, bytes) or len(public) != 32:
            raise ValueError('untrusted issuer')
        body = base64.b64decode(receipt.payload, validate=True)
        signature = base64.b64decode(receipt.signature, validate=True)
        if (base64.b64encode(body).decode('ascii') != receipt.payload
                or base64.b64encode(signature).decode('ascii') != receipt.signature
                or len(signature) != 64):
            raise ValueError('encoding')
        Ed25519PublicKey.from_public_bytes(public).verify(signature, _DOMAIN + body)
        payload = HistoricalOriginPayload.model_validate_json(body)
        if (canonical_bytes(payload) != body
                or any(value.int == 0 for value in (payload.tenant_id, payload.project_id,
                    payload.workflow_id, payload.run_id, payload.entry_id))
                or payload.target.asset_version_id.int == 0
                or payload.original_boxes is not None and payload.original_boxes.artifact_id.int == 0
                or any(value.artifact_id.int == 0 for value in (payload.original,
                    payload.prepared, payload.original_source, payload.target.source, payload.original_image))):
            raise ValueError('scope')
        if payload.contract == "WorkingImage":
            if (payload.original_image != payload.original or payload.original_boxes is not None
                    or payload.model_lock_id != "" or payload.backend != ""):
                raise ValueError("image producer pins")
        elif (len(payload.model_lock_id) != 64
                or any(c not in "0123456789abcdef" for c in payload.model_lock_id)
                or payload.backend not in ("cpu", "cuda", "mps")
                or (payload.contract == "FaceLandmarks") != (payload.original_boxes is not None)):
            raise ValueError("face producer pins")
        return payload
    except Exception as error:
        raise AdoptionInvalid('INTERCHANGE_HISTORICAL_SIGNATURE') from error


class SignedHistoricalOriginAuthority:
    """A configured verifier; each execution receives its own immutable binding."""
    def __init__(self, trusted):
        if (not isinstance(trusted, dict) or not trusted
                or any(not isinstance(key, tuple) or len(key) != 2
                    or not isinstance(key[0], UUID) or key[0].int == 0
                    or not isinstance(key[1], str) or not 1 <= len(key[1]) <= 120
                    or not isinstance(public, bytes) or len(public) != 32
                    for key, public in trusted.items())):
            raise ValueError('INTERCHANGE_HISTORICAL_TRUST_INSTALLATION')
        self.trusted = {key: bytes(public) for key, public in trusted.items()}

    def bind_inputs(self, *, entries, read, tenant_id, project_id, workflow_id,
                    run_id, plan, check_active, recheck_inputs):
        if not callable(read) or not callable(check_active) or not callable(recheck_inputs):
            raise ValueError('INTERCHANGE_HISTORICAL_INPUT_INSTALLATION')
        inputs = {(ref.artifact_id, ref.sha256): contract for ref, contract, _ in entries}
        if len(inputs) != len(entries):
            raise AdoptionInvalid('INTERCHANGE_HISTORICAL_INPUT_DUPLICATE')
        bindings = {entry.entry_id: entry.binding for entry in plan.entries if entry.binding is not None}
        receipts = {}
        for reference, contract, media in entries:
            check_active()
            if contract != 'HistoricalOriginReceipt':
                continue
            if media != 'application/json':
                raise AdoptionInvalid('INTERCHANGE_HISTORICAL_RECEIPT_MEDIA')
            raw = read(reference, 65536)
            if sha256(raw).hexdigest() != reference.sha256:
                raise AdoptionInvalid('INTERCHANGE_HISTORICAL_RECEIPT_HASH')
            payload = verify_historical_origin_receipt(raw, self.trusted)
            expected_id = uuid5(payload.workflow_id, 'interchange:historical-origin:'
                + str(payload.entry_id) + ':' + payload.contract + ':' + str(payload.original.artifact_id))
            if reference.artifact_id != expected_id:
                raise AdoptionInvalid('INTERCHANGE_HISTORICAL_RECEIPT_IDENTITY')
            if ((payload.tenant_id, payload.project_id, payload.workflow_id, payload.run_id)
                    != (tenant_id, project_id, workflow_id, run_id)
                    or bindings.get(payload.entry_id) != payload.target
                    or inputs.get((payload.prepared.artifact_id, payload.prepared.sha256)) != payload.contract):
                raise AdoptionInvalid('INTERCHANGE_HISTORICAL_RECEIPT_SCOPE')
            key = (payload.target.asset_version_id, payload.prepared.artifact_id, payload.contract)
            if key in receipts:
                raise AdoptionInvalid('INTERCHANGE_HISTORICAL_RECEIPT_DUPLICATE')
            receipts[key] = (bytes(raw), payload.model_copy(deep=True))
        recheck_inputs()
        return _BoundHistoricalOriginAuthority(self.trusted, receipts, check_active, recheck_inputs)


class _BoundHistoricalOriginAuthority:
    def __init__(self, trusted, receipts, active, recheck):
        self.trusted, self.receipts = trusted, receipts
        self.active, self.recheck = active, recheck

    def _qualified(self, binding, reference, contract):
        self.active()
        self.recheck()
        value = self.receipts.get((binding.asset_version_id, reference.artifact_id, contract))
        if value is None:
            return False
        raw, expected = value
        actual = verify_historical_origin_receipt(raw, self.trusted)
        return actual == expected and actual.target == binding and actual.prepared == reference

    def ancestors(self, _scope, _authority, plan, read, require_contract):
        ancestors = []
        for _, payload in self.receipts.values():
            if payload.contract != 'WorkingImage':
                continue
            require_contract(payload.prepared, 'WorkingImage')
            raw = read(payload.prepared, 1 << 20)
            image = WorkingImage.model_validate_json(raw)
            if canonical_bytes(image) != raw or sha256(raw).hexdigest() != payload.prepared.sha256:
                raise AdoptionInvalid('INTERCHANGE_HISTORICAL_IMAGE_HASH')
            if not self._qualified(payload.target, payload.prepared, 'WorkingImage'):
                raise AdoptionInvalid('INTERCHANGE_HISTORICAL_IMAGE_RECEIPT')
            ancestors.append((payload.target.model_copy(deep=True), payload.prepared.model_copy(deep=True), image))
        return tuple(ancestors)

    def qualify_ancestor(self, _scope, _authority, binding, reference, image):
        return (sha256(canonical_bytes(image)).hexdigest() == reference.sha256
                and self._qualified(binding, reference, 'WorkingImage'))

    def qualify_faces(self, _scope, _authority, binding, reference, landmarks, boxes, image):
        if (sha256(canonical_bytes(landmarks)).hexdigest() != reference.sha256
                or sha256(canonical_bytes(boxes)).hexdigest() != landmarks.faces.sha256
                or sha256(canonical_bytes(image)).hexdigest() != landmarks.image.sha256
                or boxes.image != landmarks.image):
            return False
        return (self._qualified(binding, reference, 'FaceLandmarks')
                and self._qualified(binding, landmarks.faces, 'FaceBoxes')
                and (landmarks.image == binding.source
                    or self._qualified(binding, landmarks.image, 'WorkingImage')))


def load_historical_origin_trust(path, expected_sha):
    """Pinned operator public-key declaration; request/archive paths are forbidden."""
    from pathlib import Path
    from photo_worker.contracts.base import parse_json
    import os
    import stat

    path = Path(path)
    if (not path.is_absolute() or '..' in path.parts
            or not isinstance(expected_sha, str) or len(expected_sha) != 64
            or any(c not in '0123456789abcdef' for c in expected_sha)):
        raise ValueError('INTERCHANGE_HISTORICAL_TRUST_FILE')
    directory = None
    try:
        directory = os.open('/', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        for component in path.parts[1:-1]:
            child = os.open(component, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
                            dir_fd=directory)
            os.close(directory)
            directory = child
        descriptor = os.open(path.name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC,
                             dir_fd=directory)
        try:
            before = os.fstat(descriptor)
            if not stat.S_ISREG(before.st_mode) or before.st_nlink != 1 or not 2 <= before.st_size <= 65536:
                raise ValueError('INTERCHANGE_HISTORICAL_TRUST_FILE')
            chunks, remaining = [], before.st_size
            while remaining:
                chunk = os.read(descriptor, min(remaining, 65536))
                if not chunk:
                    raise ValueError('INTERCHANGE_HISTORICAL_TRUST_FILE')
                chunks.append(chunk)
                remaining -= len(chunk)
            if os.read(descriptor, 1):
                raise ValueError('INTERCHANGE_HISTORICAL_TRUST_FILE')
            after = os.fstat(descriptor)
            current = os.stat(path.name, dir_fd=directory, follow_symlinks=False)
            identity = lambda item: (item.st_dev, item.st_ino, item.st_size, item.st_mtime_ns,
                                     item.st_ctime_ns, item.st_mode, item.st_nlink)
            if identity(before) != identity(after) or identity(after) != identity(current):
                raise ValueError('INTERCHANGE_HISTORICAL_TRUST_FILE_CHANGED')
            raw = b''.join(chunks)
        finally:
            os.close(descriptor)
    except OSError as error:
        raise ValueError('INTERCHANGE_HISTORICAL_TRUST_FILE') from error
    finally:
        if directory is not None:
            os.close(directory)
    if sha256(raw).hexdigest() != expected_sha:
        raise ValueError('INTERCHANGE_HISTORICAL_TRUST_PIN')
    value = parse_json(raw, max_bytes=65536)
    if (not isinstance(value, dict) or set(value) != {'schema', 'issuers'}
            or value['schema'] != 'historical-origin-trust/v1'
            or not isinstance(value['issuers'], list) or not 1 <= len(value['issuers']) <= 100):
        raise ValueError('INTERCHANGE_HISTORICAL_TRUST_FILE')
    trusted = {}
    for row in value['issuers']:
        if not isinstance(row, dict) or set(row) != {'issuer_id', 'key_id', 'public_key_hex'}:
            raise ValueError('INTERCHANGE_HISTORICAL_TRUST_FILE')
        key = (UUID(row['issuer_id']), row['key_id'])
        public = bytes.fromhex(row['public_key_hex'])
        if key in trusted or public.hex() != row['public_key_hex']:
            raise ValueError('INTERCHANGE_HISTORICAL_TRUST_DUPLICATE')
        trusted[key] = public
    return SignedHistoricalOriginAuthority(trusted)
