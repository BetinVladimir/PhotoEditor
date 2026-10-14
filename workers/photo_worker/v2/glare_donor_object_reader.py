"""Donor-mode glare streams only its own worker SQL input catalogue."""

from photo_worker.v2.portrait_donor_object_reader import DonorWorkerObjectReader


class _GlareEvidenceDatabase:
    def __init__(self, database):
        if not callable(getattr(database, 'glare_donor_input_evidence', None)):
            raise ValueError('GLARE_DONOR_EVIDENCE_UNAVAILABLE')
        self._database = database

    def donor_input_evidence(self, lease, params, confirmation, ref):
        return self._database.glare_donor_input_evidence(
            lease, params, confirmation, ref)


class GlareWorkerObjectReader(DonorWorkerObjectReader):
    """Reuse exact-version stream/hash checks with glare-specific SQL authority."""

    def __init__(self, database, lease, params_raw, confirmation_raw,
                 objects, *, bucket, check_active):
        super().__init__(_GlareEvidenceDatabase(database), lease,
                         params_raw, confirmation_raw, objects,
                         bucket=bucket, check_active=check_active)
