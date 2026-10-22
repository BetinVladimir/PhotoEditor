#!/usr/bin/env python3
"""Эталонная (одноразовая) реализация чистых функций масштабирования, документ 20.

Запуск: python3 ref_scale.py > ../scale.json   (только стандартная библиотека; целочисленная арифметика).
"""
import json

TPS = 705_600_000
MIB = 1 << 20
LIMITS_DEFAULT = {"outstanding": {"ingest": 50, "heavy": 4, "light": 8, "preview": 4},
                  "dailyCost": 14400, "shedCost": 21600}
CLASS_OF = {"ingest": "ingest", "render": "heavy", "detect_cuts": "heavy", "color_analyze": "heavy",
            "audio_analyze": "light", "preview_frames": "preview"}


def ceil_div(a, b):
    return -((-a) // b)


def job_cost_seconds(kind, duration_ticks):
    if kind not in CLASS_OF:
        return {"error": "KIND_INVALID"}
    if duration_ticks < 0 or (kind != "preview_frames" and duration_ticks == 0):
        return {"error": "DURATION_INVALID"}
    s = ceil_div(duration_ticks, TPS)
    if kind == "ingest":
        return {"cost": 30 + ceil_div(s * 3, 5)}
    if kind == "detect_cuts":
        return {"cost": 60 + ceil_div(s * 7, 20)}
    if kind == "color_analyze":
        return {"cost": 30 + ceil_div(s, 5)}
    if kind == "audio_analyze":
        return {"cost": 20 + ceil_div(s, 20)}
    if kind == "preview_frames":
        return {"cost": 5}
    return {"cost": 60 + ceil_div(s * 3, 2)}  # render


def ingest_duration_estimate_ticks(nbytes):
    if nbytes <= 0:
        return {"error": "BYTES_INVALID"}
    secs = min(10800, max(1, ceil_div(nbytes, 1_000_000)))
    return {"ticks": secs * TPS}


def retry_after_seconds(ahead, workers):
    if ahead < 0:
        return {"error": "AHEAD_INVALID"}
    w = max(1, workers)
    raw = ceil_div(ahead, w)
    raw = min(900, max(5, raw))
    return {"seconds": ceil_div(raw, 5) * 5}


def egress_lease_bytes(remaining, streams):
    if streams < 0:
        return {"error": "STREAMS_INVALID"}
    if remaining <= 0:
        return {"bytes": 0}
    if remaining < 8 * MIB:
        return {"bytes": remaining}
    chunk = remaining // (streams + 1)
    chunk = min(256 * MIB, max(8 * MIB, chunk))
    return {"bytes": min(chunk, remaining)}


def autosave_gate(now_ms, last_ms, dirty, min_interval_ms, force):
    if not dirty:
        return {"action": "idle", "waitMs": 0}
    if force or last_ms is None:
        return {"action": "save", "waitMs": 0}
    elapsed = now_ms - last_ms
    if elapsed < 0 or elapsed >= min_interval_ms:
        return {"action": "save", "waitMs": 0}
    return {"action": "wait", "waitMs": min_interval_ms - elapsed}


def admit_job(state, limits, kind, cost):
    cls = CLASS_OF.get(kind)
    if cls is None:
        return {"error": "KIND_INVALID"}
    if state["outstanding"][cls] + 1 > limits["outstanding"][cls]:
        return {"ok": False, "code": "VIDEO_TENANT_LIMIT", "limit_code": "OUTSTANDING_" + cls.upper()}
    daily = limits["dailyCost"]
    if daily is not None and state["dailyUsedCost"] + cost > daily:
        return {"ok": False, "code": "VIDEO_TENANT_LIMIT", "limit_code": "DAILY_COST"}
    if cls != "preview" and state["poolAheadCost"] > limits["shedCost"]:
        ra = retry_after_seconds(state["poolAheadCost"], state["activeWorkers"])["seconds"]
        return {"ok": False, "code": "VIDEO_CAPACITY_BUSY", "retry_after_s": ra}
    return {"ok": True}


def st(ing=0, heavy=0, light=0, prev=0, used=0, ahead=0, workers=1):
    return {"outstanding": {"ingest": ing, "heavy": heavy, "light": light, "preview": prev},
            "dailyUsedCost": used, "poolAheadCost": ahead, "activeWorkers": workers}


def main():
    out = {"_about": "Сгенерировано ref_scale.py; не редактировать вручную. Правила — документ 20, разделы 3–6.",
           "limits_default": LIMITS_DEFAULT}
    jc = []
    for kind, ticks in [("ingest", TPS), ("ingest", 60 * TPS), ("ingest", 7_620_480_000_000), ("ingest", 1),
                        ("detect_cuts", 600 * TPS), ("detect_cuts", 7_620_480_000_000),
                        ("color_analyze", 100 * TPS), ("color_analyze", 101 * TPS),
                        ("audio_analyze", 200 * TPS), ("audio_analyze", 201 * TPS),
                        ("preview_frames", 0), ("preview_frames", 5 * TPS),
                        ("render", 5 * TPS), ("render", 6 * TPS), ("render", 7_620_480_000_000),
                        ("ingest", 0), ("render", -1), ("transcode", TPS)]:
        r = job_cost_seconds(kind, ticks)
        jc.append({"kind": kind, "duration_ticks": ticks, **({"expect": r["cost"]} if "cost" in r else {"error": r["error"]})})
    out["job_cost"] = jc
    ie = []
    for b in [1, 1_000_000, 1_000_001, 1 << 30, 17179869184, 0, -1]:
        r = ingest_duration_estimate_ticks(b)
        ie.append({"bytes": b, **({"expect": r["ticks"]} if "ticks" in r else {"error": r["error"]})})
    out["ingest_duration_estimate"] = ie
    ra = []
    for a, w in [(0, 1), (1, 1), (5, 1), (6, 1), (12, 1), (100, 4), (7, 0), (100000, 2), (900, 1), (901, 1), (-1, 1)]:
        r = retry_after_seconds(a, w)
        ra.append({"ahead": a, "workers": w, **({"expect": r["seconds"]} if "seconds" in r else {"error": r["error"]})})
    out["retry_after"] = ra
    el = []
    for rem, s in [(0, 0), (-5, 1), (5 * MIB, 2), (8 * MIB, 0), (100 * MIB, 0), (100 * MIB, 3), (10 << 30, 1),
                   (20 * MIB, 10), (9 * MIB, 100), (1 << 30, 3), (1000 * MIB, 1), (100 * MIB, -1)]:
        r = egress_lease_bytes(rem, s)
        el.append({"remaining": rem, "streams": s, **({"expect": r["bytes"]} if "bytes" in r else {"error": r["error"]})})
    out["egress_lease"] = el
    ag = []
    for now, last, dirty, mn, force in [(10000, None, True, 5000, False), (10000, 4000, True, 5000, False),
                                        (10000, 5000, True, 5000, False), (10000, 5001, True, 5000, False),
                                        (10000, 9999, True, 5000, False), (10000, 9999, True, 5000, True),
                                        (10000, 9999, False, 5000, True), (10000, 20000, True, 5000, False),
                                        (0, None, False, 5000, False)]:
        ag.append({"now_ms": now, "last_ms": last, "dirty": dirty, "min_interval_ms": mn, "force": force,
                   "expect": autosave_gate(now, last, dirty, mn, force)})
    out["autosave_gate"] = ag
    ad = []
    cases = [
        ("ingest", 100, st(), None),
        ("ingest", 100, st(ing=49), None),
        ("ingest", 100, st(ing=50), None),
        ("render", 500, st(heavy=3), None),
        ("render", 500, st(heavy=4), None),
        ("detect_cuts", 500, st(heavy=4), None),
        ("audio_analyze", 40, st(light=8), None),
        ("preview_frames", 5, st(prev=4), None),
        ("preview_frames", 5, st(prev=3, used=14400, ahead=999999), None),
        ("render", 500, st(used=13900), None),
        ("render", 501, st(used=13900), None),
        ("render", 500, st(used=13900, ahead=21600), None),
        ("render", 500, st(used=0, ahead=21601, workers=4), None),
        ("render", 500, st(used=0, ahead=21601, workers=0), None),
        ("ingest", 100, st(ing=50, used=14400, ahead=999999), None),
        ("render", 500, st(used=14400, ahead=999999), None),
        ("ingest", 100, st(used=14400), {"dailyCost": None}),
        ("render", 500, st(ahead=400, workers=4), {"shedCost": 100}),
        ("ingest", 100, st(ahead=101, workers=0), {"shedCost": 100}),
        ("transcode", 1, st(), None),
    ]
    for kind, cost, state, over in cases:
        lim = json.loads(json.dumps(LIMITS_DEFAULT))
        if over:
            lim.update(over)
        r = admit_job(state, lim, kind, cost)
        item = {"kind": kind, "cost": cost, "state": state, "expect": r}
        if over:
            item["limits_override"] = over
        ad.append(item)
    out["admit_job"] = ad
    print(json.dumps(out, ensure_ascii=False, indent=1))


if __name__ == "__main__":
    main()
