"""Bounded deployment files for the isolated signer, never task-controlled.

Loading validates configuration/pins; it does not qualify SDK, keys or trust.
Native handles and independent live authorities are explicit process inputs.
"""
from dataclasses import dataclass
from hashlib import sha256
import os
from pathlib import Path
import re
import stat
from urllib.parse import urlsplit

from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.contracts.provenance import SigningPolicy, decode_provenance
from photo_worker.operations.provenance.cancellation import check_signing_active


def _trusted_owner(owner, effective=None):
    if effective is None:
        effective = os.geteuid()
    return owner in (0, effective)


def _relative(value):
    if (not isinstance(value, str) or not 1 <= len(value) <= 1024
            or value.startswith('/') or '\\' in value
            or any(part in ('', '.', '..') for part in value.split('/'))
            or any(ord(char) < 32 for char in value)):
        raise ValueError('SIGNER_DEPLOYMENT_PATH')
    return value.split('/')


def _directory(path, *, parent=None):
    check_signing_active()
    descriptor = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=parent)
    try:
        check_signing_active()
        info = os.fstat(descriptor)
        if not stat.S_ISDIR(info.st_mode) or info.st_mode & 0o022 or not _trusted_owner(info.st_uid):
            raise ValueError('SIGNER_DEPLOYMENT_DIRECTORY')
        return descriptor
    except BaseException:
        os.close(descriptor)
        raise



def _root_directory(root):
    # Pin every path component: O_NOFOLLOW on a full path protects only its
    # final component, leaving ancestor symlink replacement unchecked.
    root = Path(root)
    if not root.is_absolute() or '..' in root.parts:
        raise ValueError('SIGNER_DEPLOYMENT_ROOT')
    check_signing_active()
    descriptor = os.open('/', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        parts = root.parts[1:]
        for part in parts:
            check_signing_active()
            child = _directory(part, parent=descriptor)
            os.close(descriptor)
            descriptor = child
        info = os.fstat(descriptor)
        if not stat.S_ISDIR(info.st_mode) or info.st_mode & 0o022 or not _trusted_owner(info.st_uid):
            raise ValueError('SIGNER_DEPLOYMENT_DIRECTORY')
        check_signing_active()
        result, descriptor = descriptor, None
        return result
    finally:
        if descriptor is not None:
            os.close(descriptor)


def _read(root, name, maximum, *, secret=False):
    check_signing_active()
    parts = _relative(name)
    # Traverse descriptors, so even concurrent replacement with a symlink
    # cannot escape the operator-supplied directory. No FIFOs/devices opened.
    directory = _root_directory(root)
    try:
        for part in parts[:-1]:
            check_signing_active()
            child = _directory(part, parent=directory)
            os.close(directory)
            directory = child
        check_signing_active()
        descriptor = os.open(parts[-1], os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=directory)
        try:
            before = os.fstat(descriptor)
            if (not stat.S_ISREG(before.st_mode) or before.st_nlink != 1
                    or not _trusted_owner(before.st_uid)
                    or not 1 <= before.st_size <= maximum
                    or before.st_mode & (0o077 if secret else 0o022)):
                raise ValueError('SIGNER_DEPLOYMENT_FILE')
            chunks, remaining = [], before.st_size
            while remaining:
                check_signing_active()
                data = os.read(descriptor, min(65536, remaining))
                check_signing_active()
                if not data:
                    raise ValueError('SIGNER_DEPLOYMENT_FILE')
                chunks.append(data)
                remaining -= len(data)
            check_signing_active()
            trailing = os.read(descriptor, 1)
            check_signing_active()
            if trailing:
                raise ValueError('SIGNER_DEPLOYMENT_FILE')
            after = os.fstat(descriptor)
            check_signing_active()
            current = os.stat(parts[-1], dir_fd=directory, follow_symlinks=False)
            check_signing_active()
            identity = lambda item: (item.st_dev, item.st_ino, item.st_size,
                                     item.st_mtime_ns, item.st_ctime_ns, item.st_mode, item.st_nlink, item.st_uid)
            if identity(before) != identity(after) or identity(after) != identity(current):
                raise ValueError('SIGNER_DEPLOYMENT_FILE_CHANGED')
            check_signing_active()
            return b''.join(chunks)
        finally:
            os.close(descriptor)
    finally:
        os.close(directory)


def _digest(value):
    return isinstance(value, str) and re.fullmatch(r'[0-9a-f]{64}', value) is not None


def _hex(value, maximum):
    if (not isinstance(value, str) or not 2 <= len(value) <= 2 * maximum
            or len(value) % 2 or re.fullmatch(r'[0-9a-f]+', value) is None):
        raise ValueError('SIGNER_DEPLOYMENT_KEY_IDENTITY')
    return bytes.fromhex(value)


@dataclass(frozen=True)
class SignerDeployment:
    root: Path
    policies: tuple[bytes, ...]
    qualification_sha: str
    certificate_chain: bytes
    trust_bundle: bytes
    algorithm: str
    key_handle: str
    token_serial: bytes
    key_id: bytes
    pin_file: str
    timestamp_services: tuple[tuple[str, str], ...]
    network_hosts: tuple[str, ...]

    def pin_supplier(self):
        # Read only on key invocation. Retain neither secret bytes nor decoded
        # PIN in the deployment object; whitespace is significant, no stripping.
        try:
            return _read(self.root, self.pin_file, 1024, secret=True).decode('utf-8')
        except UnicodeError:
            raise ValueError('SIGNER_DEPLOYMENT_PIN_ENCODING') from None

    def build_executor(self, database, worker_id, artifacts, *, sdk, pkcs11,
                       library, native_authority, native_inspector, verify_binding,
                       runtime_files=None, check_active=lambda: None, on_operator=None):
        from .signer_installation import build_pkcs11_signer_executor
        from photo_worker.operations.provenance.certificate_inspector import CertificateBoundInstallationInspector
        certificate_inspector = CertificateBoundInstallationInspector(
            native_inspector, trust_bundle=self.trust_bundle)
        return build_pkcs11_signer_executor(database, worker_id, artifacts,
            self.policies, self.qualification_sha, native_authority, sdk=sdk,
            trust_bundle=self.trust_bundle, certificate_chain=self.certificate_chain,
            algorithm=self.algorithm, pkcs11=pkcs11, library=library,
            key_handle=self.key_handle, token_serial=self.token_serial, key_id=self.key_id,
            pin_supplier=self.pin_supplier, native_inspector=certificate_inspector,
            verify_binding=verify_binding, timestamp_services=dict(self.timestamp_services),
            network_hosts=self.network_hosts, runtime_files=runtime_files,
            check_active=check_active, on_operator=on_operator)


def load_signer_deployment(root, manifest='installation.json'):
    root = Path(root)
    check_signing_active()
    if not root.is_absolute() or root.resolve(strict=True) != root:
        raise ValueError('SIGNER_DEPLOYMENT_ROOT')
    document = parse_json(_read(root, manifest, 8 << 20), max_bytes=8 << 20)
    fields = {'schema_version', 'sdk_lock_sha', 'qualification_sha', 'policies',
              'certificate_chain', 'trust_bundle', 'algorithm', 'key_handle',
              'token_serial_hex', 'key_id_hex', 'pin_file', 'timestamp_services', 'network_hosts'}
    if (not isinstance(document, dict) or set(document) != fields
            or document['schema_version'] != 'signer-deployment/v1'
            or not _digest(document['sdk_lock_sha']) or not _digest(document['qualification_sha'])
            or document['algorithm'] not in ('PS256', 'PS384', 'PS512', 'ES256', 'ES384', 'ES512', 'ED25519')):
        raise ValueError('SIGNER_DEPLOYMENT_MANIFEST')
    def pinned_file(ref):
        if not isinstance(ref, dict) or set(ref) != {'path', 'sha256'} or not _digest(ref['sha256']):
            raise ValueError('SIGNER_DEPLOYMENT_PIN')
        data = _read(root, ref['path'], 1 << 20)
        if sha256(data).hexdigest() != ref['sha256']:
            raise ValueError('SIGNER_DEPLOYMENT_PIN')
        return data
    certificate = pinned_file(document['certificate_chain'])
    trust = pinned_file(document['trust_bundle'])
    values = document['policies']
    if not isinstance(values, list) or not 1 <= len(values) <= 256:
        raise ValueError('SIGNER_DEPLOYMENT_POLICIES')
    policies, ids = [], set()
    for value in values:
        raw = canonical_bytes(value)
        policy = decode_provenance(raw, SigningPolicy, 1 << 20)
        if ((policy.id, policy.revision) in ids or policy.sdk_lock_sha != document['sdk_lock_sha']
                or policy.key_handle != document['key_handle']
                or policy.certificate_chain_sha != sha256(certificate).hexdigest()
                or policy.trust_bundle_sha != sha256(trust).hexdigest()):
            raise ValueError('SIGNER_DEPLOYMENT_POLICY_PINS')
        ids.add((policy.id, policy.revision))
        policies.append(raw)
    if sum(map(len, policies)) > 8 << 20:
        raise ValueError('SIGNER_DEPLOYMENT_POLICIES')
    hosts, services = document['network_hosts'], document['timestamp_services']
    if (not isinstance(hosts, list) or len(hosts) > 64
            or any(not isinstance(host, str) or len(host) > 253
                   or re.fullmatch(r'[a-z0-9]+(?:[.-][a-z0-9]+)*', host) is None
                   or any(not 1 <= len(label) <= 63 or label.startswith('-') or label.endswith('-')
                          for label in host.split('.')) for host in hosts)
            or len(set(hosts)) != len(hosts) or not isinstance(services, dict) or len(services) > 64):
        raise ValueError('SIGNER_DEPLOYMENT_NETWORK')
    for name, url in services.items():
        if (not isinstance(name, str) or not 1 <= len(name.encode()) <= 120
                or name.strip() != name or any(c in name for c in '\x00\r\n')
                or not isinstance(url, str) or len(url) > 2048
                or any(c.isspace() or ord(c) < 32 for c in url)):
            raise ValueError('SIGNER_DEPLOYMENT_TIMESTAMP')
        parsed = urlsplit(url)
        if (parsed.scheme != 'https' or parsed.hostname not in hosts
                or parsed.username is not None or parsed.password is not None
                or parsed.fragment or parsed.port not in (None, 443)):
            raise ValueError('SIGNER_DEPLOYMENT_TIMESTAMP')
    for raw in policies:
        policy = decode_provenance(raw, SigningPolicy, 1 << 20)
        if ((policy.timestamp_mode == 'required' and policy.timestamp_service_id not in services)
                or ((policy.timestamp_mode == 'required' or policy.revocation_mode == 'required') and not hosts)):
            raise ValueError('SIGNER_DEPLOYMENT_POLICY_NETWORK')
    _relative(document['pin_file'])
    return SignerDeployment(root, tuple(policies), document['qualification_sha'],
        certificate, trust, document['algorithm'], document['key_handle'],
        _hex(document['token_serial_hex'], 64), _hex(document['key_id_hex'], 128),
        document['pin_file'], tuple(sorted(services.items())), tuple(sorted(hosts)))
