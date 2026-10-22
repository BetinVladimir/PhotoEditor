"""Install learning.evaluate only with deployment-signed qualification files."""
from __future__ import annotations

from dataclasses import dataclass
import os
from pathlib import Path
import stat

from .service import WorkerConfigurationError


@dataclass(frozen=True)
class SignedLearningRequest:
    report: bytes
    signature: bytes
    trusted_public_key: bytes
    packages_lock: Path
    evidence_root: Path
    trusted_issuer_id: str
    report_path: Path
    signature_path: Path
    public_key_path: Path


def _read_file(value: str, limit: int) -> bytes:
    path = Path(value)
    if (not path.is_absolute() or '..' in path.parts or
            not isinstance(limit, int) or limit < 1):
        raise WorkerConfigurationError('Invalid V23 qualification file')
    directory = None
    try:
        directory = os.open('/', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        for part in path.parts[1:-1]:
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
                            dir_fd=directory)
            os.close(directory)
            directory = child
        descriptor = os.open(path.name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC,
                             dir_fd=directory)
        try:
            before = os.fstat(descriptor)
            if (not stat.S_ISREG(before.st_mode) or before.st_nlink != 1 or
                    not 1 <= before.st_size <= limit):
                raise WorkerConfigurationError('Invalid V23 qualification file')
            chunks, remaining = [], before.st_size
            while remaining:
                chunk = os.read(descriptor, min(65536, remaining))
                if not chunk:
                    raise WorkerConfigurationError('Invalid V23 qualification file')
                chunks.append(chunk)
                remaining -= len(chunk)
            if os.read(descriptor, 1):
                raise WorkerConfigurationError('Invalid V23 qualification file')
            after = os.fstat(descriptor)
            current = os.stat(path.name, dir_fd=directory, follow_symlinks=False)
            identity = lambda item: (item.st_dev, item.st_ino, item.st_size,
                                     item.st_mtime_ns, item.st_ctime_ns, item.st_mode,
                                     item.st_nlink)
            if identity(before) != identity(after) or identity(after) != identity(current):
                raise WorkerConfigurationError('V23 qualification file changed')
            return b''.join(chunks)
        finally:
            os.close(descriptor)
    except OSError:
        raise WorkerConfigurationError('Invalid V23 qualification file') from None
    finally:
        if directory is not None:
            os.close(directory)


def learning_evaluation_from_environment(config, getenv=os.environ.get):
    # V23 evaluation currently has a CUDA-only implementation. Shared operator
    # configuration must not prevent supported Metal operations from starting.
    if config.resource_class == 'gpu' and config.backend == 'mps':
        return None
    names = ('PHOTO_LEARNING_QUALIFICATION_REPORT',
             'PHOTO_LEARNING_QUALIFICATION_SIGNATURE',
             'PHOTO_LEARNING_TRUSTED_PUBLIC_KEY',
             'PHOTO_LEARNING_PACKAGES_LOCK',
             'PHOTO_LEARNING_TRUSTED_ISSUER_ID',
             'PHOTO_LEARNING_EVIDENCE_ROOT')
    values = tuple(getenv(name, '') for name in names)
    if not any(values):
        return None
    if (not all(isinstance(value, str) and value for value in values) or
            config.resource_class != 'gpu' or config.backend != 'cuda' or
            config.model_root is None or config.model_lock is None or
            config.icc_root is None):
        raise WorkerConfigurationError('Incomplete V23 qualified CUDA installation')
    try:
        package_lock = Path(values[3])
        evidence_root = Path(values[5])
        if not 1 <= len(values[4]) <= 128:
            raise WorkerConfigurationError('Invalid V23 trusted issuer')
        if (not evidence_root.is_absolute() or evidence_root.is_symlink() or
                not evidence_root.is_dir()):
            raise WorkerConfigurationError('Invalid V23 evidence root')
        _read_file(values[3], 8 * 1024 * 1024)
        report = _read_file(values[0], 262144)
        signature = _read_file(values[1], 64)
        public_key = _read_file(values[2], 32)
        if len(signature) != 64 or len(public_key) != 32:
            raise WorkerConfigurationError('Invalid V23 qualification signature or key')
        return SignedLearningRequest(report, signature, public_key, package_lock,
                                     evidence_root, values[4],
                                     Path(values[0]), Path(values[1]), Path(values[2]))
    except (OSError, ValueError):
        raise WorkerConfigurationError('Invalid V23 qualification files') from None


def install_signed_learning_evaluation(request, config, pool):
    if (not isinstance(request, SignedLearningRequest) or pool is None or
            config.backend != 'cuda' or config.icc_root is None):
        raise WorkerConfigurationError('V23 qualification installation unavailable')
    from photo_worker.color.icc import ICCRegistry
    from photo_worker.operations.learning.qualified_installation import (
        signed_pinned_learning_evaluation_installation,
    )
    def verify_deployment_files():
        if (_read_file(str(request.report_path), 262144) != request.report or
                _read_file(str(request.signature_path), 64) != request.signature or
                _read_file(str(request.public_key_path), 32) !=
                request.trusted_public_key):
            raise WorkerConfigurationError('V23 qualification files changed')
    try:
        return signed_pinned_learning_evaluation_installation(
            pool, ICCRegistry(config.icc_root), report_bytes=request.report,
            signature=request.signature,
            trusted_public_key=request.trusted_public_key,
            trusted_issuer_id=request.trusted_issuer_id,
            package_root=Path(__file__).resolve().parents[1],
            packages_lock=request.packages_lock,
            evidence_root=request.evidence_root,
            verify_deployment_files=verify_deployment_files)
    except Exception:
        raise WorkerConfigurationError('V23 qualification failed') from None
