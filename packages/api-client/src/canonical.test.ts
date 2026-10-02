import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { canonicalJSON } from "./canonical";

type Vector = { input: string; canonical: string; sha256: string };
const vectors = JSON.parse(readFileSync(new URL("../../contracts/generated/canonical-vectors.json", import.meta.url), "utf8")) as Vector[];

describe("canonicalJSON", () => {
  it("matches the shared Go/Python JCS golden vectors", () => {
    for (const vector of vectors) expect(canonicalJSON(JSON.parse(vector.input))).toBe(vector.canonical);
  });

  it.each([Number.NaN, Number.POSITIVE_INFINITY, Number.NEGATIVE_INFINITY])("rejects non-interoperable number %s", value => {
    expect(() => canonicalJSON({ value })).toThrow();
  });

  it("rejects invalid Unicode and non-JSON object shapes", () => {
    expect(() => canonicalJSON("\ud800")).toThrow("JCS_INVALID_UNICODE");
    expect(() => canonicalJSON({ "\udfff": 1 })).toThrow("JCS_INVALID_UNICODE");
    expect(() => canonicalJSON(new Date(0))).toThrow("JCS_NON_PLAIN_OBJECT");
    expect(() => canonicalJSON([, 1])).toThrow("JCS_SPARSE_ARRAY");
  });

  it("rejects cycles instead of silently changing the request", () => {
    const value: { self?: unknown } = {};
    value.self = value;
    expect(() => canonicalJSON(value)).toThrow("JCS_CYCLIC_VALUE");
  });
});
