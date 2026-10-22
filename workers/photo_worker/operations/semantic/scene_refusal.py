"""Scientific refusal is a proposal outcome, never missing authority or corrupt IO."""
from contextlib import contextmanager
from dataclasses import dataclass

REASONS=frozenset({'SEMANTIC_SCENE_SUPPORT_INSUFFICIENT','SEMANTIC_SCENE_DEPTH_DEGENERATE',
    'SEMANTIC_SCENE_PROTECTED','SEMANTIC_SCENE_FOREGROUND','SEMANTIC_SCENE_NONPLANAR',
    'SEMANTIC_SCENE_PARALLAX_OR_OCCLUSION','NO_PLANAR_CORRESPONDENCE','UNSAFE_PLANAR_TRANSFORM'})


class SceneUnsupported(ValueError):
    pass


@dataclass(frozen=True)
class SceneDecision:
    reason:str|None

    def __post_init__(self):
        if self.reason is not None and self.reason not in REASONS:
            raise ValueError('SEMANTIC_SCENE_DECISION_INVALID')


@contextmanager
def scientific_scene_outcome():
    try:yield
    except ValueError as error:
        if len(error.args)==1 and error.args[0] in REASONS:
            raise SceneUnsupported(error.args[0]) from error
        raise
