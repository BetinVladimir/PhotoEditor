"""Load current SQL-pinned metadata authority, without private original IO."""
from dataclasses import dataclass
from hashlib import sha256
from typing import Literal
from uuid import UUID,uuid5
from photo_worker.contracts.base import Contract,ArtifactRef,canonical_bytes
from photo_worker.contracts.export import EncodedImage
from photo_worker.contracts.metadata import MetadataParams
from photo_worker.operations.session.contracts import VersionBinding
from .adoption import AdoptionInvalid,ArtifactRef as EvidenceRef,require_adoption_active
from .delivery_metadata_reader import MetadataResultScope
from .delivery_metadata_freeze import verify_frozen_delivery_metadata


class MetadataWriteProof(Contract):
    schema_version: Literal[1]
    task_id: UUID
    encoded: ArtifactRef
    encoded_jcs: str
    frozen_jcs: str
    frozen_sha256: str
    binding: VersionBinding
    metadata_source: ArtifactRef
    metadata_jcs: str
    policy: Literal['copyright','approved_exif']
    approved_fields: list[str]
    params: ArtifactRef
    params_jcs: str
    projection: ArtifactRef
    projection_jcs: str


@dataclass(frozen=True)
class MetadataAdoptionInput:
    proof_jcs: bytes
    proof_sha256: str
    root: EvidenceRef
    report: EvidenceRef


def restore_metadata_adoption_proof(raw,pin,scope):
    if (not isinstance(scope,MetadataResultScope) or not isinstance(raw,bytes)
            or not 2<=len(raw)<=8388608 or sha256(raw).hexdigest()!=pin):
        raise AdoptionInvalid('DELIVERY_METADATA_ADOPTION_PROOF')
    try:
        proof=MetadataWriteProof.model_validate_json(raw)
        if canonical_bytes(proof)!=raw or proof.task_id!=scope.task_id:
            raise ValueError('scope')
        frozen=verify_frozen_delivery_metadata(proof.frozen_jcs.encode(),proof.frozen_sha256,
            proof.binding,proof.metadata_source,proof.metadata_jcs.encode(),policy=proof.policy,
            approved_fields=tuple(proof.approved_fields))
        if proof.approved_fields!=frozen.approved_fields:raise ValueError('approval order')
        projection=canonical_bytes(frozen.projection)
        expected=ArtifactRef(artifact_id=uuid5(scope.task_id,'delivery:metadata:projection:1'),sha256=sha256(projection).hexdigest())
        params=MetadataParams(encoded=proof.encoded,document=expected,
            policy='copyright_only' if proof.policy=='copyright' else 'safe_camera',
            copy_gps=proof.policy=='approved_exif' and 'gps' in proof.approved_fields)
        params_raw=canonical_bytes(params)
        before=EncodedImage.model_validate_json(proof.encoded_jcs)
        if (proof.projection!=expected or proof.projection_jcs.encode()!=projection
                or proof.params_jcs.encode()!=params_raw
                or proof.params!=ArtifactRef(artifact_id=uuid5(scope.task_id,'delivery:metadata:params:1'),sha256=sha256(params_raw).hexdigest())
                or canonical_bytes(before)!=proof.encoded_jcs.encode()
                or sha256(proof.encoded_jcs.encode()).hexdigest()!=proof.encoded.sha256
                or before.metadata_policy!='strip'):
            raise ValueError('plan')
        return proof
    except (ValueError,TypeError,UnicodeError) as error:
        raise AdoptionInvalid('DELIVERY_METADATA_ADOPTION_PROOF') from error


def load_metadata_adoption_input(connection,*,scope,check_active=lambda:None):
    if not callable(check_active):
        raise AdoptionInvalid('DELIVERY_METADATA_ADOPTION_INSTALLATION')
    supplied_check_active=check_active
    def check_active():
        require_adoption_active(supplied_check_active)
    check_active()
    if connection.autocommit is not True or not isinstance(scope,MetadataResultScope):
        raise AdoptionInvalid('DELIVERY_METADATA_ADOPTION_INSTALLATION')
    with connection.transaction():
        connection.execute("SELECT set_config('app.tenant_id',%s,true)",(str(scope.tenant_id),))
        connection.execute("SELECT set_config('statement_timeout','30000',true)")
        row=connection.execute('SELECT proof_jcs,proof_sha,root_id,root_sha,report_id,report_sha '
            'FROM photo.read_v30_metadata_adoption_input(%s,%s,%s,%s,%s)',
            (scope.tenant_id,scope.task_id,scope.attempt,scope.fence,scope.result_id)).fetchone()
        check_active()
    if row is None or len(row)!=6 or not isinstance(row[0],str):
        raise AdoptionInvalid('DELIVERY_METADATA_ADOPTION_MISSING')
    raw=row[0].encode();proof=restore_metadata_adoption_proof(raw,row[1],scope)
    root=EvidenceRef.parse({'artifact_id':str(row[2]),'sha256':row[3]})
    report=EvidenceRef.parse({'artifact_id':str(row[4]),'sha256':row[5]})
    if root.artifact_id==report.artifact_id or any(ref.artifact_id in (proof.params.artifact_id,proof.encoded.artifact_id,proof.projection.artifact_id) for ref in (root,report)):
        raise AdoptionInvalid('DELIVERY_METADATA_ADOPTION_ALIAS')
    check_active();return MetadataAdoptionInput(raw,row[1],root,report)
