export type JSONPrimitive = null | boolean | number | string;
export type JSONValue = JSONPrimitive | JSONValue[] | { [key: string]: JSONValue };

function assertUnicode(value: string): void {
  for (let index = 0; index < value.length; index++) {
    const unit = value.charCodeAt(index);
    if (unit >= 0xd800 && unit <= 0xdbff) {
      const next = value.charCodeAt(++index);
      if (!(next >= 0xdc00 && next <= 0xdfff)) throw new TypeError("JCS_INVALID_UNICODE");
    } else if (unit >= 0xdc00 && unit <= 0xdfff) {
      throw new TypeError("JCS_INVALID_UNICODE");
    }
  }
}

function encode(value: unknown, ancestors: Set<object>): string {
  if (value === null) return "null";
  if (typeof value === "boolean") return value ? "true" : "false";
  if (typeof value === "string") {
    assertUnicode(value);
    return JSON.stringify(value);
  }
  if (typeof value === "number") {
    if (!Number.isFinite(value)) throw new TypeError("JCS_NON_FINITE_NUMBER");
    return Object.is(value, -0) ? "0" : JSON.stringify(value);
  }
  if (typeof value !== "object") throw new TypeError("JCS_NON_JSON_VALUE");
  if (ancestors.has(value)) throw new TypeError("JCS_CYCLIC_VALUE");
  ancestors.add(value);
  try {
    if (Array.isArray(value)) {
      const parts = new Array<string>(value.length);
      for (let index = 0; index < value.length; index++) {
        if (!(index in value)) throw new TypeError("JCS_SPARSE_ARRAY");
        parts[index] = encode(value[index], ancestors);
      }
      return `[${parts.join(",")}]`;
    }
    const prototype = Object.getPrototypeOf(value);
    if (prototype !== Object.prototype && prototype !== null) throw new TypeError("JCS_NON_PLAIN_OBJECT");
    const record = value as Record<string, unknown>;
    const keys = Object.keys(record).sort();
    const parts = keys.map(key => {
      assertUnicode(key);
      return `${JSON.stringify(key)}:${encode(record[key], ancestors)}`;
    });
    return `{${parts.join(",")}}`;
  } finally {
    ancestors.delete(value);
  }
}

/** RFC 8785/JCS serialization for values crossing the API trust boundary. */
export function canonicalJSON(value: unknown): string {
  return encode(value, new Set<object>());
}
