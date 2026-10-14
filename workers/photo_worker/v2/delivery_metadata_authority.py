"""Primary-DB metadata result authority, with short tenant-scoped transactions."""
from .adoption import AdoptionInvalid, ArtifactEvidence, ArtifactRef
from .delivery_metadata_reader import MetadataResultScope, MetadataObjectEvidence


class SQLMetadataResultAuthority:
    def __init__(self, connection, *, scope):
        if connection.autocommit is not True or not isinstance(scope,MetadataResultScope):
            raise ValueError('DELIVERY_METADATA_AUTHORITY_INSTALLATION')
        self.connection,self.scope=connection,scope

    def _read(self,scope,ref=None):
        if scope!=self.scope:raise AdoptionInvalid('DELIVERY_METADATA_AUTHORITY_SCOPE')
        args=(scope.tenant_id,scope.task_id,scope.attempt,scope.fence,scope.result_id)
        with self.connection.transaction():
            self.connection.execute("SELECT set_config('app.tenant_id',%s,true)",(str(scope.tenant_id),))
            if ref is not None:
                return self.connection.execute(
                    'SELECT bucket,object_key,object_version,sha256,bytes,media_type,role,source '
                    'FROM photo.v30_metadata_result_artifact(%s,%s,%s,%s,%s,%s)',args+(ref.artifact_id,)).fetchone()
            return self.connection.execute(
                'SELECT artifact_id,sha256,state FROM photo.v30_metadata_result_outputs(%s,%s,%s,%s,%s)',args).fetchall()

    def location(self,scope,ref):
        row=self._read(scope,ref)
        if row is None or row[3]!=ref.sha256:
            raise AdoptionInvalid('DELIVERY_METADATA_AUTHORITY_MISSING')
        return MetadataObjectEvidence(scope,ArtifactEvidence(ref,row[5],row[4],row[6],row[7]),row[0],row[1],row[2])

    def outputs(self,scope):
        rows=self._read(scope)
        if len(rows)!=3 or any(row[2]!='ready' for row in rows):
            raise AdoptionInvalid('DELIVERY_METADATA_AUTHORITY_OUTPUTS')
        refs=tuple(ArtifactRef.parse({'artifact_id':str(row[0]),'sha256':row[1]}) for row in rows)
        if len({ref.artifact_id for ref in refs})!=3:
            raise AdoptionInvalid('DELIVERY_METADATA_AUTHORITY_OUTPUTS')
        return refs
