"""V29 multi-source planar registration; no rendering or source authority.

Matrices map integer pixel centres from source i to source j. Callers supply
verified linear-luma proxies and pin this module and all imported dependencies
in the qualified runtime lock before dispatching an operation.
"""
from dataclasses import dataclass
from math import isfinite
import cv2
import numpy as np
from .registration import _RANSAC_LOCK
from .semantic_transfer import match_planar_proxies


@dataclass(frozen=True)
class RegisteredPair:
    i: int
    j: int
    matrix: np.ndarray
    inliers: int
    matches: int


def _matrix(value, shape):
    h = np.array(value, dtype=np.float64, copy=True)
    if h.shape != (3, 3) or not np.isfinite(h).all() or abs(h[2, 2]) <= 1e-10:
        raise ValueError('UNSAFE_REGISTRATION')
    h /= h[2, 2]
    if abs(np.linalg.det(h)) <= 1e-10 or np.linalg.cond(h) > 1e8:
        raise ValueError('UNSAFE_REGISTRATION')
    height, width = shape
    corners = np.array([[0, 0, 1], [width-1, 0, 1],
                        [width-1, height-1, 1], [0, height-1, 1]], dtype=np.float64)
    denominator = (h @ corners.T)[2]
    if not (np.all(denominator > 1e-8) or np.all(denominator < -1e-8)):
        raise ValueError('UNSAFE_REGISTRATION')
    h.flags.writeable = False
    return h


def _shapes(shapes):
    if not isinstance(shapes, (list, tuple)) or not 2 <= len(shapes) <= 30:
        raise ValueError('REGISTRATION_INPUT_INVALID')
    owned = tuple(tuple(shape) for shape in shapes)
    if any(len(s) != 2 or any(type(v) is not int or v < 1 for v in s)
           or s[0]*s[1] > 200000000 for s in owned):
        raise ValueError('REGISTRATION_INPUT_INVALID')
    return owned


def register_pairs(proxies, source_shapes, *, registration_error_px,
                   match_inlier_min, reference=None, masks=None, check_active=lambda: None):
    """Lexicographic all-pairs panorama or direct-reference focus matching.

    The RANSAC threshold is in proxy pixels as required by the calibration.
    Failed pairs are absent; graph/direct-reference assembly rejects omissions.
    """
    shapes = _shapes(source_shapes)
    if (reference is None and len(shapes)>20 or len(proxies) != len(shapes) or type(registration_error_px) not in (int, float)
            or not isfinite(registration_error_px) or not .01 <= registration_error_px <= 10
            or type(match_inlier_min) not in (int, float)
            or not isfinite(match_inlier_min) or not 0 <= match_inlier_min <= 1
            or reference is not None and (type(reference) is not int or not 0 <= reference < len(shapes))):
        raise ValueError('REGISTRATION_CONFIG_INVALID')
    owned = []
    for proxy, shape in zip(proxies, shapes):
        check_active()
        if (not isinstance(proxy, np.ndarray) or proxy.dtype != np.uint8 or proxy.ndim != 2
                or any(not 8 <= v <= 3048 for v in proxy.shape)):
            raise ValueError('REGISTRATION_PROXY_INVALID')
        owned.append(np.array(proxy, copy=True, order='C'))
    if masks is None:
        masks = [None]*len(shapes)
    elif not isinstance(masks, (list, tuple)) or len(masks) != len(shapes):
        raise ValueError('REGISTRATION_PROXY_INVALID')
    owned_masks = []
    for mask, proxy in zip(masks, owned):
        if mask is not None and (not isinstance(mask,np.ndarray) or mask.dtype!=np.uint8 or mask.shape!=proxy.shape):
            raise ValueError('REGISTRATION_PROXY_INVALID')
        owned_masks.append(None if mask is None else np.array(mask,copy=True))
    result = []
    for i in range(len(shapes)):
        for j in range(i+1, len(shapes)):
            if reference is not None and reference not in (i, j):
                continue
            check_active()
            points = match_planar_proxies(owned[i], owned[j], reference_mask=owned_masks[i], target_mask=owned_masks[j], check_active=check_active)
            count = len(points.reference)
            if count < 12:
                continue
            with _RANSAC_LOCK:
                check_active(); threads = cv2.getNumThreads()
                try:
                    cv2.setNumThreads(1); cv2.setRNGSeed(2026)
                    matrix, mask = cv2.findHomography(points.reference, points.target,
                        cv2.RANSAC, float(registration_error_px), maxIters=2000, confidence=.995)
                finally:
                    cv2.setNumThreads(threads)
            check_active()
            inliers = 0 if mask is None else int(np.count_nonzero(mask))
            if matrix is None or inliers/count < match_inlier_min:
                continue
            def scale(k):
                ph, pw = owned[k].shape; sh, sw = shapes[k]
                # Resize uses half-pixel centre correspondence.
                sx, sy = pw/sw, ph/sh
                return np.array([[sx, 0, (sx-1)/2], [0, sy, (sy-1)/2], [0, 0, 1.]])
            try:
                full = _matrix(np.linalg.inv(scale(j)) @ matrix @ scale(i), shapes[i])
                _matrix(np.linalg.inv(full), shapes[j])
            except (ValueError, np.linalg.LinAlgError):
                continue
            result.append(RegisteredPair(i, j, full, inliers, count))
    return tuple(result)


def assemble_registrations(pairs, source_shapes, reference, *, direct=False,
                           registration_error_px=1., check_active=lambda: None):
    """Return source→reference matrices and inconsistent-cycle review flag."""
    shapes = _shapes(source_shapes); n = len(shapes)
    if (type(reference) is not int or not 0 <= reference < n or type(direct) is not bool
            or type(registration_error_px) not in (int, float)
            or not isfinite(registration_error_px) or not .01 <= registration_error_px <= 10):
        raise ValueError('REGISTRATION_CONFIG_INVALID')
    if not isinstance(pairs, (list, tuple)) or len(pairs) > n*(n-1)//2:
        raise ValueError('REGISTRATION_PAIR_INVALID')
    edges = []; seen = set()
    for pair in pairs:
        check_active()
        if (not isinstance(pair, RegisteredPair) or type(pair.i) is not int or type(pair.j) is not int
                or not 0 <= pair.i < pair.j < n or (pair.i, pair.j) in seen
                or type(pair.inliers) is not int or type(pair.matches) is not int
                or not 1 <= pair.inliers <= pair.matches <= 4000):
            raise ValueError('REGISTRATION_PAIR_INVALID')
        seen.add((pair.i, pair.j))
        h = _matrix(pair.matrix, shapes[pair.i]); inv = _matrix(np.linalg.inv(h), shapes[pair.j])
        edges.append((pair, h, inv))
    selected = []
    if direct:
        selected = [e for e in edges if reference in (e[0].i, e[0].j)]
        if len(selected) != n-1:
            raise ValueError('UNREGISTERED_FRAME')
    else:
        parent = list(range(n))
        def root(i):
            while parent[i] != i:
                i = parent[i]
            return i
        for edge in sorted(edges, key=lambda e: (-e[0].inliers, e[0].i, e[0].j)):
            a, b = root(edge[0].i), root(edge[0].j)
            if a != b:
                parent[a] = b; selected.append(edge)
        if len(selected) != n-1:
            raise ValueError('UNREGISTERED_FRAME')
    matrices = [None]*n; matrices[reference] = _matrix(np.eye(3), shapes[reference])
    while any(h is None for h in matrices):
        check_active(); progress = False
        for pair, h, inv in selected:
            if matrices[pair.i] is not None and matrices[pair.j] is None:
                matrices[pair.j] = _matrix(matrices[pair.i] @ inv, shapes[pair.j]); progress = True
            elif matrices[pair.j] is not None and matrices[pair.i] is None:
                matrices[pair.i] = _matrix(matrices[pair.j] @ h, shapes[pair.i]); progress = True
        if not progress:
            raise ValueError('UNREGISTERED_FRAME')
    inconsistent = False
    for pair, h, _ in edges:
        check_active(); height, width = shapes[pair.i]
        points = np.array([[0, 0, 1], [width-1, 0, 1], [width-1, height-1, 1], [0, height-1, 1]]).T
        a = matrices[pair.i] @ points; b = matrices[pair.j] @ h @ points
        errors = np.linalg.norm(a[:2]/a[2] - b[:2]/b[2], axis=0)
        if np.median(errors) > 2*registration_error_px:
            inconsistent = True
    return tuple(matrices), inconsistent
