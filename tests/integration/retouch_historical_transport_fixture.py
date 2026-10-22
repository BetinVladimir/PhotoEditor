"""Historical SQL reconstruction writer; current callers retain canonical JSON."""
import json
import rfc8785

def manifest_bytes(body, *, historical_writer_head=None):
    if historical_writer_head not in (None, 2406, 2415):
        raise ValueError('RETOUCH_HISTORICAL_WRITER_HEAD_INVALID')
    if historical_writer_head is None:
        return rfc8785.dumps(body)
    if set(body) != {'chunks','entry_count','entries_sha','schema_version','task_id'}:
        raise ValueError('RETOUCH_HISTORICAL_MANIFEST_SHAPE_INVALID')
    # Exact 02362/02393 SQL reconstruction, before physical reserve/complete.
    ordered={key:body[key] for key in ('chunks','entry_count','entries_sha','schema_version','task_id')}
    # Nested chunk key order must also match SQL artifact_id,sha256.
    ordered['chunks']=[{'artifact_id':c['artifact_id'],'sha256':c['sha256']} for c in body['chunks']]
    return json.dumps(ordered,separators=(',', ':'),ensure_ascii=False).encode()
