#!/usr/bin/env python3
"""Эталонная (одноразовая) реализация FN-X-01..03, документ 22.

Запуск: python3 -I ref_license.py > ../license.json   (только стандартная библиотека).
"""
import json
import re

RANK = {"allow": 0, "conditional": 1, "review": 2, "deny": 3}
NAME = {v: k for k, v in RANK.items()}

ALLOW = {"mit", "mit-0", "bsd-2-clause", "bsd-3-clause", "bsd-3-clause-clear", "isc", "apache-2.0", "zlib",
         "psf-2.0", "python-2.0", "0bsd", "unlicense", "cc0-1.0", "bsl-1.0", "hpnd", "ncsa", "wtfpl",
         "licenseref-public-domain", "licenseref-own-footage", "licenseref-own-work"}
ATTRIBUTION = {"cc-by-3.0", "cc-by-4.0"}
CONDITIONAL = {"lgpl-2.1-only", "lgpl-2.1-or-later", "lgpl-3.0-only", "lgpl-3.0-or-later", "mpl-2.0",
               "gpl-2.0-only", "gpl-2.0-or-later", "gpl-3.0-only", "gpl-3.0-or-later"}
REVIEW = {"creativeml-openrail-m", "openrail", "openrail++", "bigscience-openrail-m", "llama2", "llama3",
          "gemma", "cc-by-sa-3.0", "cc-by-sa-4.0", "epl-2.0", "other"}
DENY = {
    "agpl-3.0-only": "NETWORK_COPYLEFT", "agpl-3.0-or-later": "NETWORK_COPYLEFT",
    "sspl-1.0": "NETWORK_COPYLEFT", "busl-1.1": "RESTRICTED_USE",
    "cc-by-nc-3.0": "NON_COMMERCIAL", "cc-by-nc-4.0": "NON_COMMERCIAL",
    "cc-by-nc-sa-3.0": "NON_COMMERCIAL", "cc-by-nc-sa-4.0": "NON_COMMERCIAL",
    "cc-by-nc-nd-3.0": "NON_COMMERCIAL", "cc-by-nc-nd-4.0": "NON_COMMERCIAL",
    "cc-by-nd-3.0": "NO_DERIVATIVES", "cc-by-nd-4.0": "NO_DERIVATIVES",
    "non-commercial": "NON_COMMERCIAL", "research-only": "RESTRICTED_USE",
    "proprietary": "RESTRICTED_USE", "all-rights-reserved": "NO_LICENSE_GRANT",
    "unlicensed": "NO_LICENSE_GRANT", "none": "NO_LICENSE_GRANT", "unknown": "NO_LICENSE_GRANT",
}
BARE = {"gpl-2.0": "gpl-2.0-only", "gpl-3.0": "gpl-3.0-only", "lgpl-2.1": "lgpl-2.1-only",
        "lgpl-3.0": "lgpl-3.0-only", "agpl-3.0": "agpl-3.0-only"}
PLUS = {"gpl-2.0": "gpl-2.0-or-later", "gpl-3.0": "gpl-3.0-or-later", "lgpl-2.1": "lgpl-2.1-or-later",
        "lgpl-3.0": "lgpl-3.0-or-later", "agpl-3.0": "agpl-3.0-or-later"}


class Bad(Exception):
    pass


def leaf(ident, kind):
    i = ident.lower()
    if i.endswith("+") and i[:-1] in PLUS:
        i = PLUS[i[:-1]]
    i = BARE.get(i, i)
    if i in ALLOW:
        return 0, []
    if i in ATTRIBUTION:
        return 0, ["ATTRIBUTION_REQUIRED"]
    if i in CONDITIONAL:
        if kind == "code":
            return 1, ["COPYLEFT_CONDITIONS"]
        return 2, ["COPYLEFT_ON_NON_CODE"]
    if i in REVIEW:
        return 2, ["NEEDS_HUMAN_REVIEW"]
    if i in DENY:
        return 3, [DENY[i]]
    if i.startswith("licenseref-"):
        return 2, ["NEEDS_HUMAN_REVIEW"]
    return 2, ["LICENSE_UNKNOWN"]


def dedup(xs):
    out = []
    for x in xs:
        if x not in out:
            out.append(x)
    return out


def parse(tokens, kind):
    pos = [0]

    def peek():
        return tokens[pos[0]] if pos[0] < len(tokens) else None

    def take():
        t = peek()
        pos[0] += 1
        return t

    def is_op(t, w):
        return t is not None and t.lower() == w

    def factor():
        t = take()
        if t is None or t == ")" or is_op(t, "and") or is_op(t, "or") or is_op(t, "with"):
            raise Bad()
        if t == "(":
            r = expr()
            if take() != ")":
                raise Bad()
            return r
        r = leaf(t, kind)
        if is_op(peek(), "with"):
            take()
            e = take()
            if e is None or e in "()" or is_op(e, "and") or is_op(e, "or") or is_op(e, "with"):
                raise Bad()
        return r

    def term():
        r = factor()
        while is_op(peek(), "and"):
            take()
            n = factor()
            r = (max(r[0], n[0]), dedup(r[1] + n[1]))
        return r

    def expr():
        r = term()
        while is_op(peek(), "or"):
            take()
            n = term()
            if n[0] < r[0]:
                r = n
        return r

    res = expr()
    if peek() is not None:
        raise Bad()
    return res


def license_verdict(expr, kind):
    if kind not in ("code", "weights", "data"):
        return {"error": "KIND_INVALID"}
    s = (expr or "").strip()
    if not s:
        return {"verdict": "deny", "reasons": ["NO_LICENSE_GRANT"]}
    toks = re.findall(r"\(|\)|[^\s()]+", s)
    try:
        rank, reasons = parse(toks, kind)
    except Bad:
        return {"verdict": "deny", "reasons": ["EXPR_INVALID"]}
    return {"verdict": NAME[rank], "reasons": dedup(reasons)}


def model_gate(e):
    reasons = []
    vc = license_verdict(e.get("code_license"), "code")["verdict"]
    if vc not in ("allow", "conditional"):
        reasons.append("CODE_" + vc.upper())
    vw = license_verdict(e.get("weights_license"), "weights")["verdict"]
    if vw != "allow":
        reasons.append("WEIGHTS_" + vw.upper())
    d = e.get("training_data_license")
    if d is None or not str(d).strip():
        reasons.append("DATA_NOT_RECORDED")
    else:
        vd = license_verdict(d, "data")["verdict"]
        if vd != "allow":
            reasons.append("DATA_" + vd.upper())
    if e.get("redistribution_policy") == "unresolved":
        reasons.append("REDISTRIBUTION_UNRESOLVED")
    return {"eligible": not reasons, "reasons": reasons}


NONFREE = {"nonfree", "libfdk-aac", "openssl", "libnpp", "cuda-nvcc", "cuda-sdk", "decklink"}
GPLF = {"gpl", "libx264", "libx265", "libxvid", "libvidstab", "librubberband", "frei0r", "libcdio",
        "libxavs", "libxavs2", "libdavs2", "postproc"}


def ffmpeg_build_verdict(buildconf, profile):
    if profile not in ("lgpl", "gpl_internal"):
        return {"error": "PROFILE_INVALID"}
    out = []
    for tok in buildconf.split():
        if not tok.lower().startswith("--enable-"):
            continue
        name = tok[len("--enable-"):].split("=")[0].lower().replace("_", "-")
        if name in NONFREE:
            v = "NONFREE:" + tok
        elif name in GPLF and profile == "lgpl":
            v = "GPL:" + tok
        else:
            continue
        if v not in out:
            out.append(v)
    return {"ok": not out, "violations": out}


def main():
    out = {"_about": "Сгенерировано ref_license.py; не редактировать вручную. Правила — документ 22, разделы 2 и 6."}
    lv = []
    cases = [
        ("MIT", "code"), ("mit", "weights"), ("Apache-2.0", "data"), ("BSD-3-Clause", "code"),
        ("ISC", "code"), ("CC0-1.0", "data"), ("CC-BY-4.0", "data"), ("CC-BY-3.0", "weights"),
        ("LGPL-2.1-or-later", "code"), ("LGPL-2.1+", "code"), ("LGPL-2.1-or-later", "weights"),
        ("GPL-2.0", "code"), ("GPL-3.0+", "code"), ("GPL-3.0-only", "data"), ("MPL-2.0", "code"),
        ("AGPL-3.0-only", "code"), ("AGPL-3.0", "code"), ("CC-BY-NC-4.0", "weights"),
        ("CC-BY-NC-SA-4.0", "data"), ("CC-BY-ND-4.0", "data"), ("CC-BY-SA-4.0", "data"),
        ("CreativeML-OpenRAIL-M", "weights"), ("other", "weights"), ("research-only", "weights"),
        ("LicenseRef-Public-Domain", "data"), ("LicenseRef-Own-Footage", "data"), ("LicenseRef-Own-Work", "weights"),
        ("LicenseRef-CC-Unspecified", "data"), ("LicenseRef-ClipShots-Unstated", "data"),
        ("", "code"), ("   ", "weights"), ("none", "weights"), ("SomethingNew-1.0", "code"),
        ("MIT OR GPL-3.0-only", "code"), ("GPL-3.0-only OR MIT", "code"),
        ("MIT AND CC-BY-NC-4.0", "weights"), ("MIT AND Apache-2.0", "code"),
        ("MIT OR Apache-2.0 AND GPL-2.0-only", "code"), ("(MIT OR GPL-2.0-only) AND BSD-3-Clause", "code"),
        ("GPL-2.0-or-later WITH Classpath-exception-2.0", "code"), ("MIT WITH", "code"),
        ("MIT AND", "code"), ("(MIT", "code"), ("MIT)", "code"), ("MIT MIT", "code"),
        ("mit or apache-2.0", "weights"), ("Apache-2.0 AND LicenseRef-CC-Unspecified", "data"),
        ("MIT", "model"),
    ]
    for e, k in cases:
        lv.append({"expr": e, "kind": k, "expect": license_verdict(e, k)})
    out["license_verdict"] = lv

    mg = []
    base = {"code_license": "MIT", "weights_license": "MIT", "training_data_license": "CC-BY-4.0",
            "redistribution_policy": "allowed"}
    variants = [
        {}, {"redistribution_policy": "restricted"}, {"redistribution_policy": "unresolved"},
        {"training_data_license": None}, {"training_data_license": ""},
        {"training_data_license": "LicenseRef-CC-Unspecified AND LicenseRef-ClipShots-Unstated"},
        {"training_data_license": "CC-BY-NC-4.0"}, {"weights_license": "CreativeML-OpenRAIL-M"},
        {"weights_license": "LGPL-2.1-or-later"}, {"code_license": "LGPL-2.1-or-later"},
        {"code_license": "AGPL-3.0-only", "weights_license": "none", "training_data_license": "unknown",
         "redistribution_policy": "unresolved"},
        {"code_license": "Apache-2.0", "weights_license": "Apache-2.0", "training_data_license": "LicenseRef-Own-Footage AND CC0-1.0"},
        {"weights_license": "LicenseRef-Own-Work", "training_data_license": "CC-BY-3.0 AND CC-BY-4.0 AND CC0-1.0 AND LicenseRef-Own-Footage"},
        {"weights_license": "LicenseRef-Own-Work", "training_data_license": "CC-BY-3.0 AND LicenseRef-CC-Unspecified"},
    ]
    for v in variants:
        e = dict(base)
        e.update(v)
        mg.append({"entry": e, "expect": model_gate(e)})
    out["model_gate"] = mg

    fb = []
    lgpl_conf = ("--prefix=/opt/ffmpeg --disable-gpl --disable-nonfree --enable-version3 --enable-ffnvcodec "
                 "--enable-nvenc --enable-shared --disable-static")
    confs = [
        (lgpl_conf, "lgpl"), (lgpl_conf, "gpl_internal"), ("", "lgpl"),
        ("configuration: --enable-gpl --enable-libx264", "lgpl"),
        ("configuration: --enable-gpl --enable-libx264", "gpl_internal"),
        ("--enable-nonfree --enable-libfdk-aac", "gpl_internal"),
        ("--enable-nonfree --enable-libfdk_aac", "lgpl"),
        ("--enable-openssl --enable-libnpp --enable-cuda-nvcc", "lgpl"),
        ("--enable-libx264=shared --enable-libx265", "lgpl"),
        ("--enable-libx264 --enable-libx264", "lgpl"),
        ("--disable-gpl --disable-libx264 --disable-nonfree", "lgpl"),
        ("--enable-libopenh264 --enable-libvpx --enable-libsvtav1 --enable-libopus", "lgpl"),
        ("--enable-postproc --enable-libvidstab --enable-librubberband --enable-frei0r", "lgpl"),
        ("--enable-postproc --enable-libvidstab", "gpl_internal"),
        ("--ENABLE-GPL", "lgpl"), ("--enable-GPL", "lgpl"), ("enable-gpl", "lgpl"),
        ("--enable-gpl", "windows"), ("--enable-decklink --enable-gpl", "lgpl"),
        ("--enable-libxavs2 --enable-libdavs2 --enable-libxvid --enable-libcdio", "lgpl"),
    ]
    for c, p in confs:
        fb.append({"buildconf": c, "profile": p, "expect": ffmpeg_build_verdict(c, p)})
    out["ffmpeg_build"] = fb
    print(json.dumps(out, ensure_ascii=False, indent=1))


if __name__ == "__main__":
    main()
