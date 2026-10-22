"""Normal adopter constructor; task authority is rebound for every result."""
import os
from photo_worker.runtime.semantic_scene_config import scene_operator_configuration
from .semantic_scene_adoption import SemanticSceneAdoption


def semantic_scene_adopter_from_environment(config,getenv=os.environ.get):
    configured=scene_operator_configuration(getenv)
    if configured is None:return None
    raw=getenv('PHOTO_SEMANTIC_TRANSFER_ADOPTION_RAM_BYTES','')
    if not isinstance(raw,str) or not raw.isascii() or not raw.isdecimal():
        raise ValueError('SEMANTIC_ADOPTION_RAM_REQUIRED')
    ram=int(raw)
    if not 1<=ram<=2**53-1:raise ValueError('SEMANTIC_ADOPTION_RAM_INVALID')
    measurements,verify=configured
    verifier=SemanticSceneAdoption(measurements,verify,config.layer_scratch_root,config.layer_scratch_bytes,ram)
    return verifier,ram
