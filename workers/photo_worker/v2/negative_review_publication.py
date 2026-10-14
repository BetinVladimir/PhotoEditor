"""Publish negative candidate reviews from version-pinned original Params."""
from hashlib import sha256
from uuid import UUID

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.optical_negative import OpticalNegativeParams


class NegativeReviewPublisher:
    def __init__(self, read, write, objects, bucket, *, check_active):
        if not bucket or not callable(check_active):
            raise ValueError('NEGATIVE_REVIEW_CONFIGURATION')
        self.read, self.write, self.objects = read, write, objects
        self.bucket, self.check_active, self.after = bucket, check_active, None

    def process_pending(self):
        self.check_active()
        rows = self.read.execute('SELECT * FROM photo.pending_v29_negative_reviews(%s)',
                                 (self.after,)).fetchall()
        if len(rows) > 100:
            raise ValueError('NEGATIVE_REVIEW_PAGE_INVALID')
        if not rows:
            self.after = None
        published, retries = 0, 0
        for row in rows:
            self.check_active()
            try:
                tenant, task, root, candidate, digest, bucket, key, version, size = row
                self.after = task
                if (any(not isinstance(value, UUID) or value.int == 0 for value in
                        (tenant, task, root, candidate)) or bucket != self.bucket
                        or not key or not version or version == 'null'
                        or type(size) is not int or not 2 <= size <= 65536):
                    raise ValueError('NEGATIVE_REVIEW_OBJECT_INVALID')
                response = self.objects.get_object(bucket, key, version_id=version)
                try:
                    parts, total = [], 0
                    while total <= 65536:
                        self.check_active()
                        block = response.read(min(8192, 65537-total))
                        if not block:
                            break
                        total += len(block)
                        parts.append(block)
                    raw = b''.join(parts)
                finally:
                    try:
                        response.close()
                    finally:
                        response.release_conn()
                self.check_active()
                if len(raw) != size or sha256(raw).hexdigest() != digest:
                    raise ValueError('NEGATIVE_REVIEW_PARAMS_HASH')
                params = OpticalNegativeParams.model_validate_json(raw)
                if canonical_bytes(params) != raw:
                    raise ValueError('NEGATIVE_REVIEW_PARAMS_NONCANONICAL')
                with self.write.transaction():
                    self.check_active()
                    result = self.write.execute(
                        'SELECT photo.create_v29_negative_review(%s,%s,%s,%s,%s,%s)',
                        (tenant, task, root, candidate, params.base.source.artifact_id,
                         raw.decode('utf-8'))).fetchone()
                    if result and result[0] is not None:
                        published += 1
                    else:
                        retries += 1
            except Exception:
                # Authority/availability failures stay retryable; never publish
                # raw Params, object paths or exception details into run events.
                self.check_active()
                retries += 1
        return published, retries
