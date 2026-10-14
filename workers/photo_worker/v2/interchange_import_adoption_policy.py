"""Load process-owned qualification selections for imported recipes.

The file is a deployment pin, never a substitute for the live SQL/object
report check or the independent semantic callback.
"""

from hashlib import sha256
import os
import re
from uuid import UUID

from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.runtime.learning_config import _read_file
from .adoption import AdoptionInvalid
from .interchange_import_adoption_qualification_report import validate_import_adoption_expected
from .interchange_import_adoption_runtime import ImportAdoptionQualification


def import_adoption_qualifications_from_environment(getenv=os.environ.get):
    prefix = 'PHOTO_ADOPTER_INTERCHANGE_IMPORT_QUALIFICATIONS_'
    path, digest = getenv(prefix + 'FILE', ''), getenv(prefix + 'SHA', '')
    if not path and not digest:
        return None
    if (not isinstance(path, str) or not os.path.isabs(path)
            or not isinstance(digest, str)
            or re.fullmatch('[0-9a-f]{64}', digest) is None):
        raise ValueError('INTERCHANGE_IMPORT_QUALIFICATIONS_PIN')
    try:
        raw = _read_file(path, 1 << 20)
        if sha256(raw).hexdigest() != digest:
            raise ValueError('pin mismatch')
        document = parse_json(raw, max_bytes=1 << 20)
        if (canonical_bytes(document) != raw or not isinstance(document, dict)
                or set(document) != {'schema_version', 'qualifications'}
                or type(document['schema_version']) is not int
                or document['schema_version'] != 1
                or not isinstance(document['qualifications'], list)
                or not 1 <= len(document['qualifications']) <= 5):
            raise ValueError('invalid document')
        result = {}
        for item in document['qualifications']:
            if not isinstance(item, dict) or set(item) != {
                    'operation', 'scale', 'expected', 'report_id', 'report_sha'}:
                raise ValueError('invalid qualification')
            operation, scale = item['operation'], item['scale']
            if operation == 'image.upscale':
                if type(scale) is not int or scale not in (2, 4):
                    raise ValueError('invalid scale')
                key = (operation, scale)
            elif operation in ('image.denoise', 'color.stats', 'mask.person'):
                if scale is not None:
                    raise ValueError('unexpected scale')
                key = operation
            else:
                raise ValueError('invalid operation')
            expected = item['expected']
            validate_import_adoption_expected(expected)
            if expected['Operation'] != operation or key in result:
                raise ValueError('qualification conflict')
            report_id = UUID(item['report_id'])
            report_sha = item['report_sha']
            if (report_id.int == 0 or str(report_id) != item['report_id']
                    or not isinstance(report_sha, str)
                    or re.fullmatch('[0-9a-f]{64}', report_sha) is None):
                raise ValueError('invalid report ref')
            result[key] = ImportAdoptionQualification(expected, report_id, report_sha)
        return result
    except (OSError, TypeError, ValueError, KeyError, AdoptionInvalid) as error:
        raise ValueError('INTERCHANGE_IMPORT_QUALIFICATIONS_PIN') from error
