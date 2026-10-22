"""Work candidate: exact GetObject denial through actual physical preview provider.

No SQL mutations, stored-object deletion, fake receipt or production callback
replacement. Caller first completes its existing real producer in owned MinIO.
"""
import os
from pathlib import Path
from uuid import uuid4
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic_preview import (
    SemanticRemovalPreviewInput, SemanticBackgroundCleanupPreviewInput,
    SemanticRetouchPreviewInput,
)


def assert_preview_late_access_loss(*, repo, directory, environment, go, family,
                                    params, actor, session, srgb_sha):
    types = {'removal': SemanticRemovalPreviewInput,
             'background_cleanup': SemanticBackgroundCleanupPreviewInput,
             'retouch': SemanticRetouchPreviewInput}
    if family not in types or not isinstance(params, types[family]):
        raise ValueError('closed typed physical preview family required')
    root = Path(repo).resolve()
    target = Path(directory).resolve()
    if not target.is_relative_to(root) or target == root or Path(directory).is_symlink():
        raise ValueError('owned repository fixture directory required')
    path = target / ('late-access-' + uuid4().hex + '.json')
    body = canonical_bytes(dict(family=family, params=params.model_dump(mode='json'),
                                actor=str(actor), session=str(session), srgb_sha=srgb_sha))
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, 'wb') as stream:
        stream.write(body)
    key = 'PHOTO_TEST_PREVIEW_LATE_ACCESS_FIXTURE'
    sentinel = object()
    previous = environment.get(key, sentinel)
    environment[key] = str(path)
    try:
        go('./internal/httpapi', 'TestIntegrationPreviewPinnedAccessLossHTTP',
           'V27_PREVIEW_PINNED_ACCESS_LOSS_HTTP_OK')
    finally:
        if previous is sentinel:
            environment.pop(key, None)
        else:
            environment[key] = previous
    # Keep private identity document for owner evidence. It contains no DSN,
    # storage credentials, signed URLs or worker capability token.
