"""Exact scheduler locator for current import model qualification reports."""

from dataclasses import dataclass
from contextlib import closing
from hashlib import sha256
from uuid import UUID

from psycopg.types.json import Jsonb

from photo_worker.contracts.base import canonical_bytes, parse_json
from .adoption import AdoptionInvalid


@dataclass(frozen=True)
class ImportQualificationObject:
    artifact_id: UUID
    sha256: str
    bucket: str
    object_key: str
    object_version: str
    bytes: int


class SQLImportAdoptionQualificationCatalog:
    OPERATIONS = frozenset(('image.denoise', 'image.upscale',
                            'color.stats', 'mask.person'))

    def __init__(self, connection, allowed_bucket, check_active):
        if (connection.autocommit is not True or not isinstance(allowed_bucket, str)
                or not allowed_bucket or not callable(check_active)):
            raise ValueError('INTERCHANGE_IMPORT_QUALIFICATION_CATALOG_INSTALLATION')
        self.connection, self.bucket, self.active = connection, allowed_bucket, check_active

    def lookup(self, scope, operation, expected, report_id, report_sha):
        if (not isinstance(scope, tuple) or len(scope) != 5
                or not isinstance(scope[0], UUID) or scope[0].int == 0
                or not isinstance(scope[1], UUID) or scope[1].int == 0
                or type(scope[2]) is not int or scope[2] < 1
                or type(scope[3]) is not int or scope[3] < 1
                or not isinstance(scope[4], UUID) or scope[4].int == 0
                or operation not in self.OPERATIONS
                or not isinstance(report_id, UUID) or report_id.int == 0
                or not isinstance(report_sha, str) or len(report_sha) != 64
                or any(c not in '0123456789abcdef' for c in report_sha)):
            raise AdoptionInvalid('INTERCHANGE_IMPORT_QUALIFICATION_SCOPE')
        try:
            owned = parse_json(canonical_bytes(expected), max_bytes=1 << 20)
        except (TypeError, ValueError) as error:
            raise AdoptionInvalid('INTERCHANGE_IMPORT_QUALIFICATION_POLICY') from error
        if (not isinstance(owned, dict) or owned.get('Operation') != operation
                or owned.get('Version') != '1'
                or owned.get('LicenseCleared') is not True):
            raise AdoptionInvalid('INTERCHANGE_IMPORT_QUALIFICATION_POLICY')
        self.active()
        rows = self.connection.execute(
            'SELECT artifact_id,sha256,bucket,object_key,object_version,bytes '
            'FROM photo.v30_import_adoption_qualification_object(%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            scope + (operation, Jsonb(owned), report_id, report_sha)).fetchall()
        self.active()
        if not isinstance(rows, list) or len(rows) != 1:
            raise AdoptionInvalid('INTERCHANGE_IMPORT_QUALIFICATION_UNAVAILABLE')
        row = rows[0]
        if (not isinstance(row, (tuple, list)) or len(row) != 6
                or row[0] != report_id or row[1] != report_sha
                or row[2] != self.bucket or not isinstance(row[3], str)
                or not 1 <= len(row[3]) <= 1024
                or not isinstance(row[4], str) or not 1 <= len(row[4]) <= 1024
                or type(row[5]) is not int or not 2 <= row[5] <= 8 << 20):
            raise AdoptionInvalid('INTERCHANGE_IMPORT_QUALIFICATION_OBJECT')
        return ImportQualificationObject(*row)

    def recheck(self, scope, operation, expected, report_id, report_sha, pinned):
        if (not isinstance(pinned, ImportQualificationObject)
                or self.lookup(scope, operation, expected, report_id, report_sha) != pinned):
            raise AdoptionInvalid('INTERCHANGE_IMPORT_QUALIFICATION_CHANGED')

    def read_report(self, objects, scope, operation, expected, report_id, report_sha):
        if not callable(getattr(objects, 'get_object', None)):
            raise ValueError('INTERCHANGE_IMPORT_QUALIFICATION_OBJECT_STORE')
        pinned = self.lookup(scope, operation, expected, report_id, report_sha)
        self.active()
        response = objects.get_object(pinned.bucket, pinned.object_key,
                                      version_id=pinned.object_version)
        try:
            with closing(response):
                data = bytearray()
                while len(data) <= pinned.bytes:
                    self.active()
                    block = response.read(min(65536, pinned.bytes + 1 - len(data)))
                    if not isinstance(block, bytes):
                        raise AdoptionInvalid('INTERCHANGE_IMPORT_QUALIFICATION_STREAM')
                    if not block:
                        break
                    data.extend(block)
                self.active()
                if len(data) != pinned.bytes:
                    raise AdoptionInvalid('INTERCHANGE_IMPORT_QUALIFICATION_SIZE')
                raw = bytes(data)
        finally:
            release = getattr(response, 'release_conn', None)
            if callable(release):
                release()
        if sha256(raw).hexdigest() != pinned.sha256:
            raise AdoptionInvalid('INTERCHANGE_IMPORT_QUALIFICATION_HASH')
        try:
            document = parse_json(raw, max_bytes=8 << 20)
            if not isinstance(document, dict) or canonical_bytes(document) != raw:
                raise ValueError('noncanonical report')
        except (TypeError, ValueError) as error:
            raise AdoptionInvalid('INTERCHANGE_IMPORT_QUALIFICATION_CANONICAL') from error
        self.recheck(scope, operation, expected, report_id, report_sha, pinned)
        return raw
