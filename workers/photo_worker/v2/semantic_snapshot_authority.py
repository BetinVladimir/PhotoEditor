"""Load a frozen snapshot through live scheduler-scoped catalog authority."""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.semantic import EditIntent
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.session.contracts import InputSnapshot
from .adoption import ArtifactRef,AdoptionInvalid,MAX_JSON_BYTES


def load_semantic_snapshot(connection,tenant_id,task_id,context,reader,intent,*,check_active=lambda:None):
    if not connection.autocommit:raise ValueError('Snapshot preparation requires autocommit')
    intent=EditIntent.model_validate_json(canonical_bytes(intent));check_active()
    scope=(tenant_id,task_id,context.attempt,context.fence,context.result_id)
    query='SELECT * FROM photo.v27_semantic_assembly_snapshot(%s,%s,%s,%s,%s)'
    rows=connection.execute(query,scope).fetchall()
    if not 1<=len(rows)<=5000:raise AdoptionInvalid('SEMANTIC_SNAPSHOT_SCOPE_STALE')
    pin=rows[0][:4]
    if any(row[:4]!=pin or type(row[4]) is not int or row[4]!=n for n,row in enumerate(rows)):
        raise AdoptionInvalid('SEMANTIC_SNAPSHOT_EVIDENCE_INVALID')
    root=ArtifactRef.parse({'artifact_id':str(pin[0]),'sha256':pin[1]})
    def document(ref,model):
        check_active();evidence=reader.evidence(ref)
        if evidence.ref!=ref or evidence.source!='input' or evidence.media_type!='application/json':
            raise AdoptionInvalid('SEMANTIC_SNAPSHOT_EVIDENCE_INVALID')
        raw=reader.read_json(ref,MAX_JSON_BYTES);check_active()
        if (not isinstance(raw,bytes) or len(raw)>MAX_JSON_BYTES or len(raw)!=evidence.bytes
                or sha256(raw).hexdigest()!=ref.sha256):raise AdoptionInvalid('SEMANTIC_SNAPSHOT_BYTES_INVALID')
        try:return model.model_validate_json(raw)
        except ValueError as error:raise AdoptionInvalid('SEMANTIC_SNAPSHOT_CONTRACT_INVALID') from error
    snapshot=document(root,InputSnapshot)
    if (snapshot.snapshot_id!=pin[2] or snapshot.snapshot_id!=intent.target_snapshot_id
            or snapshot.snapshot_digest!=pin[3] or len(snapshot.items)!=len(rows)
            or canonical_hash(snapshot.model_dump(mode='json',exclude={'snapshot_digest'}))!=pin[3]):
        raise AdoptionInvalid('SEMANTIC_SNAPSHOT_CHANGED')
    for item,row in zip(snapshot.items,rows):
        check_active()
        if (item.ordinal!=row[4] or item.binding.asset_version_id!=row[5]
                or item.binding.source.artifact_id!=row[6] or item.binding.source.sha256!=row[7]
                or item.working_image!=item.binding.source):raise AdoptionInvalid('SEMANTIC_SNAPSHOT_CHANGED')
        image=document(ArtifactRef(item.binding.source.artifact_id,item.binding.source.sha256),WorkingImage)
        if canonical_hash(image.canvas)!=item.binding.canvas_hash:raise AdoptionInvalid('SEMANTIC_SNAPSHOT_CANVAS_CHANGED')
    check_active()
    if connection.execute(query,scope).fetchall()!=rows:
        raise AdoptionInvalid('SEMANTIC_SNAPSHOT_SCOPE_STALE')
    check_active();return snapshot


def semantic_snapshot_authority(connection,*,check_active=lambda:None):
    if not connection.autocommit or not callable(check_active):raise ValueError('Snapshot authority configuration invalid')
    def load(tenant_id,task_id,context,reader,intent):
        return load_semantic_snapshot(connection,tenant_id,task_id,context,reader,intent,check_active=check_active)
    return load
