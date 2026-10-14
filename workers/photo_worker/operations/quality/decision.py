"""Shared technical quality thresholds for production and result validation."""
def _decision(metrics,reference,icc=True,orientation=True):
    reasons=[];failure=False
    if not metrics.finite:reasons.append('nonfinite');failure=True
    if metrics.clipped_black_fraction>.02:reasons.append('excessive_black_clipping');failure|=metrics.clipped_black_fraction>.1
    if metrics.clipped_white_fraction>.02:reasons.append('excessive_white_clipping');failure|=metrics.clipped_white_fraction>.1
    if reference is not None and reference.protected_max_delta is not None and reference.protected_max_delta>2/65535:reasons.append('protected_pixels_changed');failure=True
    if metrics.seam_p99_delta>.03:reasons.append('seam_detected')
    if metrics.sharpness_laplacian<1e-6:reasons.append('low_sharpness')
    if not icc:reasons.append('icc_mismatch');failure=True
    if not orientation:reasons.append('orientation_mismatch');failure=True
    return ('fail' if failure else 'review' if reasons else 'pass'),reasons

