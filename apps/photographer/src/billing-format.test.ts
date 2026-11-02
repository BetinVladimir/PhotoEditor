import {describe, expect, it} from 'vitest';
import {formatGB, formatGiB, formatTB, formatMoney, parseBillingInt, quotaProgress} from './billing-format';

describe('parseBillingInt', () => {
  it('parses a decimal string without precision loss above MAX_SAFE_INTEGER', () => {
    const large = '9007199254740993'; // MAX_SAFE_INTEGER + 2
    expect(parseBillingInt(large)).toBe(9007199254740993n);
  });
  it('parses zero and negative values', () => {
    expect(parseBillingInt('0')).toBe(0n);
    expect(parseBillingInt('-42')).toBe(-42n);
  });
  it('throws on non-integer strings', () => {
    expect(() => parseBillingInt('1.5')).toThrow('BILLING_INT_INVALID');
    expect(() => parseBillingInt('')).toThrow('BILLING_INT_INVALID');
    expect(() => parseBillingInt('abc')).toThrow('BILLING_INT_INVALID');
  });
});

describe('formatGB vs formatGiB — distinct units', () => {
  it('formatGB uses decimal 10^9 divisor', () => {
    expect(formatGB(1_000_000_000n)).toBe('1.00 GB');
    expect(formatGB(1_500_000_000n)).toBe('1.50 GB');
  });
  it('formatGiB uses binary 2^30 divisor', () => {
    expect(formatGiB(1_073_741_824n)).toBe('1.00 GiB');
    expect(formatGiB(2_147_483_648n)).toBe('2.00 GiB');
  });
  it('GB and GiB values differ for same byte count (criterion A)', () => {
    const bytes = 2_000_000_000n; // 2 GB but ~1.86 GiB
    expect(formatGB(bytes)).toBe('2.00 GB');
    expect(formatGiB(bytes)).not.toBe('2.00 GiB');
    expect(formatGiB(bytes)).toBe('1.86 GiB');
  });
  it('formatGB works for large values without precision loss (criterion B)', () => {
    // 10 petabytes in bytes, safely above Number.MAX_SAFE_INTEGER
    const tenPB = 10_000_000_000_000_000n;
    expect(formatTB(tenPB)).toBe('10000.00 TB');
  });
  it('throws on negative bytes', () => {
    expect(() => formatGB(-1n)).toThrow('BILLING_BYTES_NEGATIVE');
    expect(() => formatGiB(-1n)).toThrow('BILLING_BYTES_NEGATIVE');
  });
});

describe('formatMoney', () => {
  it('formats EUR cents in English locale', () => {
    expect(formatMoney('1050', 'EUR', 'en-US')).toBe('€10.50');
  });
  it('preserves the full signed int64 amount without rounding cents',()=>{
    expect(formatMoney('9223372036854775807','EUR','en-US')).toBe('€92,233,720,368,547,758.07');
    expect(formatMoney('-1','EUR','en-US')).toBe('-€0.01');
    expect(formatMoney('-1050','EUR','en-US')).toBe('-€10.50');
  });
  it('handles zero amount', () => {
    expect(formatMoney('0', 'EUR', 'en-US')).toBe('€0.00');
  });
});

describe('quotaProgress', () => {
  it('returns value in [0, 1] for partial use (criterion C)', () => {
    const p = quotaProgress(50n, 100n);
    expect(p).toBeGreaterThanOrEqual(0);
    expect(p).toBeLessThanOrEqual(1);
    expect(p).toBe(0.5);
  });
  it('clamps to 1 when used equals granted', () => {
    expect(quotaProgress(100n, 100n)).toBe(1);
  });
  it('clamps to 1 when used exceeds granted (bar does not exceed 1)', () => {
    expect(quotaProgress(150n, 100n)).toBe(1);
  });
  it('returns 0 when used is 0', () => {
    expect(quotaProgress(0n, 100n)).toBe(0);
  });
  it('returns 1 when granted is 0 (fully depleted sentinel)', () => {
    expect(quotaProgress(0n, 0n)).toBe(1);
  });
});
