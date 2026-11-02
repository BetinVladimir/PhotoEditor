/** Pure formatting utilities for billing numbers. No PII touches these functions. */

/** Parse a bigint string from the server without float precision loss. */
export function parseBillingInt(value: string): bigint {
  if (!/^-?[0-9]+$/.test(value)) throw new Error('BILLING_INT_INVALID');
  return BigInt(value);
}

/**
 * Format a monetary minor-unit string (e.g. "1050" → "€10.50" for EUR).
 * Uses locale-aware Intl.NumberFormat and bigint throughout, preserving signed int64 cents.
 */
export function formatMoney(grossMinor: string, currency: string, locale: string): string {
  const minor = parseBillingInt(grossMinor),negative=minor<0n,absolute=negative?-minor:minor;
  const whole=absolute/100n,fraction=absolute%100n;
  const formatter=new Intl.NumberFormat(locale,{style:'currency',currency,minimumFractionDigits:2,maximumFractionDigits:2});
  const amount=negative?(whole===0n?-0:-whole):whole;
  const digits=new Intl.NumberFormat(locale,{useGrouping:false,minimumIntegerDigits:2,maximumFractionDigits:0}).format(fraction);
  return formatter.formatToParts(amount).map(part=>part.type==='fraction'?digits:part.value).join('');
}

/**
 * Format bytes as decimal gigabytes (1 GB = 1,000,000,000 bytes).
 * Uses bigint arithmetic to avoid float precision loss.
 */
export function formatGB(bytes: bigint): string {
  if (bytes < 0n) throw new Error('BILLING_BYTES_NEGATIVE');
  const gb = bytes / 1_000_000_000n;
  const dec = (bytes % 1_000_000_000n) * 100n / 1_000_000_000n;
  return `${gb}.${String(dec).padStart(2, '0')} GB`;
}

/**
 * Format bytes as binary gibibytes (1 GiB = 1,073,741,824 bytes = 2^30).
 * Uses bigint arithmetic to avoid float precision loss.
 * Must be displayed separately from GB — the division factor differs.
 */
export function formatGiB(bytes: bigint): string {
  if (bytes < 0n) throw new Error('BILLING_BYTES_NEGATIVE');
  const gib_divisor = 1_073_741_824n;
  const gib = bytes / gib_divisor;
  const dec = (bytes % gib_divisor) * 100n / gib_divisor;
  return `${gib}.${String(dec).padStart(2, '0')} GiB`;
}

/**
 * Format bytes as decimal terabytes (1 TB = 1,000,000,000,000 bytes).
 * Uses bigint arithmetic to avoid float precision loss.
 */
export function formatTB(bytes: bigint): string {
  if (bytes < 0n) throw new Error('BILLING_BYTES_NEGATIVE');
  const gb = bytes / 1_000_000_000_000n;
  const dec = (bytes % 1_000_000_000_000n) * 100n / 1_000_000_000_000n;
  return `${gb}.${String(dec).padStart(2, '0')} TB`;
}

/**
 * Compute ProgressBar progress in the range [0, 1].
 * Uses bigint division to avoid float precision loss.
 * Returns 1.0 if granted is zero (treat as full/depleted).
 */
export function quotaProgress(used: bigint, granted: bigint): number {
  if (granted <= 0n) return 1;
  if (used <= 0n) return 0;
  if (used >= granted) return 1;
  // Multiply numerator by 10000 for 4 decimal places of precision
  return Number(used * 10_000n / granted) / 10_000;
}
