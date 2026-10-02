from collections.abc import Sequence
import numpy as np
from photo_worker.contracts.color_fit import ColorFit
from photo_worker.contracts.style import GlobalColor, LocalColor, Disabled, LUT


def _median(values: Sequence[float]) -> float:
    return float(np.median(np.asarray(values, dtype=np.float64)))


def aggregate_color_fits(fits: Sequence[ColorFit]) -> tuple[GlobalColor, LocalColor | Disabled, dict]:
    if not 1 <= len(fits) <= 32:
        raise ValueError("FIT_COUNT_INVALID")
    pair_ids = [str(fit.params.pair_id) for fit in fits]
    if len(pair_ids) != len(set(pair_ids)):
        raise ValueError("DUPLICATE_PAIR_FIT")
    templates = [fit.global_color for fit in fits]
    first = templates[0]
    if any(item.residual_lut.size != first.residual_lut.size or item.residual_lut.encoding != first.residual_lut.encoding for item in templates):
        raise ValueError("FIT_LUT_INCOMPATIBLE")
    global_color = GlobalColor(
        gains_rgb=[_median([item.gains_rgb[i] for item in templates]) for i in range(3)],
        master_curve=[_median([item.master_curve[i] for item in templates]) for i in range(7)],
        rgb_curves=[[_median([item.rgb_curves[channel][i] for item in templates]) for i in range(7)] for channel in range(3)],
        saturation=_median([item.saturation for item in templates]),
        residual_lut=LUT(kind="residual", size=first.residual_lut.size, order="RGB_C", domain=[0., 1.],
                         encoding=first.residual_lut.encoding,
                         values=[_median([item.residual_lut.values[i] for item in templates]) for i in range(len(first.residual_lut.values))]),
    )
    reliable = [fit.local for fit in fits if fit.local.enabled and not fit.warnings]
    if len(reliable) * 2 >= len(fits):
        local = LocalColor(enabled=True,
            person_ev=_median([item.person_ev for item in reliable]), background_ev=_median([item.background_ev for item in reliable]),
            person_saturation=_median([item.person_saturation for item in reliable]), background_saturation=_median([item.background_saturation for item in reliable]),
            mask_policy_id="person_guided_v1")
    else:
        local = Disabled(enabled=False)
    evidence = {
        "pair_ids": pair_ids,
        "accepted_local_fits": len(reliable),
        "median_holdout_loss": _median([fit.holdout_loss for fit in fits]),
        "median_delta_e": _median([fit.median_delta_e for fit in fits]),
        "p95_delta_e": _median([fit.p95_delta_e for fit in fits]),
        "warnings": sorted({warning for fit in fits for warning in fit.warnings}),
    }
    return global_color, local, evidence
