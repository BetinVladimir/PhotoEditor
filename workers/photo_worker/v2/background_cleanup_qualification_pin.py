"""BackgroundCleanup qualification identity; report verification remains mandatory."""
from uuid import UUID
from .semantic_qualification_pin import SemanticQualificationPin,_sha
from .adoption import AdoptionInvalid,ArtifactRef


class BackgroundCleanupQualificationPin(SemanticQualificationPin):
    def __post_init__(self):
        if (self.backend not in ('cpu','cuda','mps') or any(not isinstance(value,UUID) or value.int==0 for value in
                (self.policy_id,self.qualification_id,self.issuer_id))
                or any(not _sha(value) for value in (self.policy_sha,self.model_lock_sha,
                    self.runtime_lock_sha,self.device_profile_sha,self.dataset_sha))
                or any(not isinstance(ref,ArtifactRef) or not isinstance(ref.artifact_id,UUID)
                    or ref.artifact_id.int==0 or not _sha(ref.sha256) for ref in (self.report,self.calibration))
                or self.report.artifact_id==self.calibration.artifact_id):
            raise AdoptionInvalid('SEMANTIC_QUALIFICATION_PIN_INVALID')
