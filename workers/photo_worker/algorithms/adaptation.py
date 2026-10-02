from __future__ import annotations

from dataclasses import dataclass
import math

import numpy as np

from photo_worker.contracts.style import Adaptation, Disabled, LocalColor
from .color import LUMA, expand_lut


@dataclass(frozen=True)
class Stats20:
    values: np.ndarray
    coverage_bins: np.ndarray


def coverage_indices(pixels: np.ndarray) -> np.ndarray:
    bins = np.minimum((np.clip(pixels,0,1)*8).astype(np.int32),7)
    return (bins[...,0]*8+bins[...,1])*8+bins[...,2]


def compute_stats20(linear_rgb: np.ndarray, person: np.ndarray,
                    valid: np.ndarray | None = None) -> Stats20:
    rgb = np.asarray(linear_rgb,dtype=np.float64)
    if rgb.ndim != 3 or rgb.shape[-1] != 3 or not np.isfinite(rgb).all():
        raise ValueError("Stats20 requires finite linear HxWx3 RGB")
    if person.shape != rgb.shape[:2] or not np.isfinite(person).all():
        raise ValueError("Person mask shape/values invalid")
    if valid is None:
        valid = np.ones(rgb.shape[:2],dtype=bool)
    if valid.shape != rgb.shape[:2]:
        raise ValueError("Valid mask shape mismatch")
    valid = valid.astype(bool)
    pixels = rgb[valid]
    if not len(pixels):
        raise ValueError("EMPTY_VALID_REGION")
    y_all = rgb@LUMA
    y = y_all[valid]
    chroma = np.ptp(pixels,axis=1)
    features = [*np.percentile(y,[5,25,50,75,95]), *pixels.mean(axis=0),
                *pixels.std(axis=0),chroma.mean(),chroma.std(),np.mean(y<.02),
                np.mean(y>.98),np.mean(person[valid]>=.5)]
    h,w = rgb.shape[:2]
    for ys,xs in [(slice(0,h//2),slice(0,w//2)),(slice(0,h//2),slice(w//2,w)),
                  (slice(h//2,h),slice(0,w//2)),(slice(h//2,h),slice(w//2,w))]:
        q = y_all[ys,xs][valid[ys,xs]]
        features.append(float(q.mean()) if q.size else float(y.mean()))
    coverage = np.zeros(512,dtype=bool)
    coverage[coverage_indices(pixels)] = True
    return Stats20(np.asarray(features,dtype=np.float64),coverage)


def fit_normalizer(features: np.ndarray) -> tuple[np.ndarray,np.ndarray]:
    f = np.asarray(features,dtype=np.float64)
    if f.ndim != 2 or f.shape[1] != 20 or not len(f) or not np.isfinite(f).all():
        raise ValueError("Expected nonempty finite Nx20 features")
    return f.mean(axis=0),np.maximum(f.std(axis=0),.05)


@dataclass(frozen=True)
class Neighbor:
    prototype_id: str
    distance: float
    weight: float


def neighbors(features: np.ndarray, adaptation: Adaptation) -> list[Neighbor]:
    f = np.asarray(features,dtype=np.float64)
    if f.shape != (20,) or not np.isfinite(f).all():
        raise ValueError("Expected finite Stats20 vector")
    ordered = sorted(adaptation.prototypes,key=lambda p:p.id)
    vectors = np.asarray([p.features for p in ordered],dtype=np.float64)
    distance = np.sqrt(np.mean(((f-vectors)/adaptation.scale)**2,axis=1))
    equal = np.flatnonzero(np.all(vectors==f,axis=1))
    if len(equal):
        p = ordered[int(equal[0])]
        return [Neighbor(p.id,0,1)]
    indices = np.argsort(distance,kind="stable")[:adaptation.k]
    log_weights = -.5 * distance[indices]**2
    weights = np.exp(log_weights-log_weights.max())
    weights /= weights.sum()
    return [Neighbor(ordered[int(i)].id,float(distance[i]),float(w)) for i,w in zip(indices,weights)]


def blend(adaptation: Adaptation, selected: list[Neighbor]) -> tuple[np.ndarray,LocalColor]:
    by_id = {p.id:p for p in adaptation.prototypes}
    if not selected or not math.isclose(sum(n.weight for n in selected),1,abs_tol=1e-12):
        raise ValueError("Neighbor weights must sum to one")
    size = adaptation.prototypes[0].absolute_lut.size
    grid = np.zeros((size,size,size,3),dtype=np.float64)
    local = np.zeros(4,dtype=np.float64)
    for n in selected:
        if n.weight < 0 or n.prototype_id not in by_id:
            raise ValueError("Invalid neighbor")
        p = by_id[n.prototype_id]
        grid += n.weight*expand_lut(p.absolute_lut)
        params = p.local
        values = [0,0,1,1] if isinstance(params,Disabled) else [params.person_ev,params.background_ev,params.person_saturation,params.background_saturation]
        local += n.weight*np.asarray(values)
    return grid.astype(np.float32),LocalColor(enabled=True,person_ev=float(local[0]),background_ev=float(local[1]),person_saturation=float(np.clip(local[2],.7,1.3)),background_saturation=float(np.clip(local[3],.7,1.3)),mask_policy_id="person_guided_v1")


def ood_reasons(features: np.ndarray, pixels: np.ndarray, adaptation: Adaptation) -> dict:
    flat = np.asarray(pixels).reshape(-1,3)
    if not len(flat) or not np.isfinite(flat).all():
        raise ValueError("No finite valid pixels for OOD")
    counts=np.bincount(coverage_indices(flat),minlength=512)
    return ood_reasons_from_counts(features,counts,adaptation)


def ood_reasons_from_counts(features:np.ndarray,counts:np.ndarray,adaptation:Adaptation)->dict:
    histogram=np.asarray(counts)
    if (histogram.shape!=(512,) or histogram.dtype.kind not in 'iu'
        or np.any(histogram<0) or np.any(histogram>200000000)):
        raise ValueError('OOD_COVERAGE_COUNTS_INVALID')
    histogram=histogram.astype(np.int64)
    total=int(histogram.sum())
    if not 1<=total<=200000000:raise ValueError('OOD_COVERAGE_COUNTS_INVALID')
    selected=neighbors(features,adaptation)
    uncovered=float(histogram[~np.asarray(adaptation.coverage_bins)].sum()/total)
    reasons = []
    if selected[0].distance > adaptation.ood_threshold:
        reasons.append("ood_distance")
    if uncovered > .2:
        reasons.append("ood_color_coverage")
    return {"fallback":bool(reasons),"distance":selected[0].distance,"uncovered_fraction":uncovered,"reasons":reasons}


def farthest_prototypes(ids: list[str], features: np.ndarray, limit: int) -> list[int]:
    if limit < 1 or len(ids)!=len(features) or len(set(ids))!=len(ids):
        raise ValueError("Invalid prototype selection input")
    _, scale = fit_normalizer(features)
    ordered = sorted(range(len(ids)),key=lambda i:ids[i])
    selected = [ordered[0]]
    while len(selected) < min(limit,len(ids)):
        remaining = [i for i in ordered if i not in selected]
        def nearest(i):
            return min(float(np.mean(((features[i]-features[j])/scale)**2)) for j in selected)
        selected.append(max(remaining,key=nearest))
    return selected


def calibrate_ood(features: np.ndarray, sessions: list[str]) -> tuple[float,str]:
    if len(features)!=len(sessions):
        raise ValueError("Session count mismatch")
    _,scale=fit_normalizer(features)
    if len(set(sessions))<2:
        return 1.,"uncalibrated"
    distances=[]
    for i,f in enumerate(features):
        others=np.asarray([features[j] for j in range(len(features)) if sessions[j]!=sessions[i]])
        distances.append(float(np.sqrt(np.mean(((others-f)/scale)**2,axis=1)).min()))
    return max(1.,float(np.percentile(distances,95))*1.5),"calibrated"
