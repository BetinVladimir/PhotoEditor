"""Own exact pinned JSON bytes for the dedicated locked SQL commit."""
from .adoption import ArtifactRef,AdoptionInvalid,require_adoption_active
from .shown_exposure_installation import ShownExposureAdoptionInstallation
from .shown_exposure_adoption import verify_shown_exposure_output
from hashlib import sha256


def shown_exposure_commit_inputs(reader,installation,root,*,check_active):
    if not isinstance(installation,ShownExposureAdoptionInstallation):
        raise AdoptionInvalid('SEMANTIC_SHOWN_INSTALLATION_REQUIRED')
    params,_,_=verify_shown_exposure_output(reader,installation.params,root,
        verify_admission=installation.verify_admission,check_active=check_active)
    def raw(ref):
        require_adoption_active(check_active)
        value=reader.read_json(ref,1048576)
        require_adoption_active(check_active)
        if not isinstance(value,bytes) or not 2<=len(value)<=1048576 or sha256(value).hexdigest()!=ref.sha256:
            raise AdoptionInvalid('SEMANTIC_SHOWN_BYTES_INVALID')
        return value.decode('utf-8')
    body=raw(installation.params);output=raw(root)
    shown=raw(ArtifactRef(params.working_image.artifact_id,params.working_image.sha256))
    if installation.verify_admission(body.encode('utf-8')) is not True:
        raise AdoptionInvalid('SEMANTIC_SHOWN_ADMISSION_DENIED')
    require_adoption_active(check_active)
    return (installation.params.artifact_id,installation.params.sha256,body,
            root.artifact_id,root.sha256,output,shown)
