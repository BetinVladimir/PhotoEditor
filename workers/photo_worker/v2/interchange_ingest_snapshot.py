"""Canonical ingest snapshot from independently verified parse output evidence."""
from pathlib import PurePosixPath
from .adoption import AdoptionInvalid, ArtifactRef
from .interchange_parse import ParsedInterchange, validate_parsed_inventory
from photo_worker.contracts.base import canonical_bytes

_SUFFIXES = frozenset('.jpg .jpeg .png .tif .tiff .nef .nrw .crw .cr2 .cr3 .arw .srf .sr2 .mrw .dng .rw2 .orf .raf .pef .srw .3fr .fff .iiq .mos .kdc .mef .x3f'.split())


def prepare_imported_ingest_snapshot(result_jcs, reader, *, check_active=lambda: None):
    check_active()
    parsed = ParsedInterchange.model_validate_json(result_jcs)
    try:
        validate_parsed_inventory(parsed)
    except ValueError as error:
        raise AdoptionInvalid('INTERCHANGE_INGEST_INVENTORY') from error
    files = {item.path: item for item in parsed.files}
    if len(files) != len(parsed.files):
        raise AdoptionInvalid('INTERCHANGE_INGEST_DUPLICATE_PATH')
    items = []
    for ordinal, entry in enumerate(parsed.manifest.entries):
        check_active()
        file = files.get(entry.image_path)
        if file is None or file.content.sha256 != entry.image_sha256:
            raise AdoptionInvalid('INTERCHANGE_INGEST_SOURCE_MISMATCH')
        reference = ArtifactRef(file.content.artifact_id, file.content.sha256)
        evidence = reader.evidence(reference)
        check_active()
        if (evidence.ref != reference or evidence.source != 'output'
                or evidence.role != 'imported_file' or evidence.media_type != 'application/octet-stream'
                or type(evidence.bytes) is not int or evidence.bytes < 1):
            raise AdoptionInvalid('INTERCHANGE_INGEST_EVIDENCE_INVALID')
        suffix = PurePosixPath(entry.image_path).suffix.lower()
        if suffix not in _SUFFIXES or evidence.bytes > 536870912:
            continue
        items.append({'ordinal':ordinal,'artifact_id':str(reference.artifact_id),
            'sha256':reference.sha256,'bytes':evidence.bytes,
            'payload':{'schema_version':1,'suffix':suffix,'assume_profile':None,'analyze':False,'tile':512}})
    check_active()
    return canonical_bytes(items)
