"""Live worker donor input metadata plus exact versioned object access."""
from copy import deepcopy
from .adoption import AdoptionInvalid,ArtifactEvidence
from .adoption_store import PostgresObjectEvidenceReader,_Location


class DonorWorkerObjectReader(PostgresObjectEvidenceReader):
    """Reuse bounded versioned streams; obtain every location from worker SQL.

    Database calls must finish before object reads. No metadata cache or
    scheduler-only adoption query is used; outputs cannot be enumerated.
    """
    def __init__(self,database,lease,params_raw,confirmation_raw,objects,*,bucket,check_active):
        if (not callable(check_active) or not callable(getattr(database,'donor_input_evidence',None))
                or not isinstance(bucket,str) or not bucket):raise ValueError('DONOR_AUTHORITY')
        if (not isinstance(params_raw,bytes) or not 2<=len(params_raw)<=65536
                or not isinstance(confirmation_raw,bytes) or not 2<=len(confirmation_raw)<=65536):
            raise ValueError('DONOR_DOCUMENT_INVALID')
        self._database=database;self._lease=deepcopy(lease)
        self._params=bytes(params_raw);self._proof=bytes(confirmation_raw)
        self.objects=objects;self.allowed_bucket=bucket;self._check=check_active

    def _location(self,ref):
        self._check()
        row=self._database.donor_input_evidence(deepcopy(self._lease),self._params,self._proof,ref)
        self._check()
        if (not isinstance(row,dict) or set(row)!={'bucket','object_key','object_version','sha256','bytes','media_type','role','source'}
                or row['bucket']!=self.allowed_bucket or row['sha256']!=ref.sha256 or row['source']!='input'
                or type(row['bytes']) is not int or not 1<=row['bytes']<=2**53-1
                or any(not isinstance(row[k],str) or not row[k] for k in ('object_key','object_version','media_type'))
                or row['object_version']=='null' or row['role'] is not None and not isinstance(row['role'],str)):
            raise AdoptionInvalid('DONOR_INPUT_EVIDENCE_INVALID')
        return _Location(row['bucket'],row['object_key'],row['object_version'],
            ArtifactEvidence(ref,row['media_type'],row['bytes'],row['role'],'input'))

    def staged_outputs(self):
        raise AdoptionInvalid('DONOR_INPUT_EVIDENCE_INVALID')
