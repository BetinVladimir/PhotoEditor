"""Derived parameters consumed by effect adapters, never a stored StyleV2."""
from typing import Literal
from .base import Contract, SHA256, Unit
from .resolved import ResolvedModules

class EffectiveParams(Contract):
    version: Literal['effective_params_v1'] = 'effective_params_v1'
    resolved_hash: SHA256
    color_strength: Unit
    modules: ResolvedModules
