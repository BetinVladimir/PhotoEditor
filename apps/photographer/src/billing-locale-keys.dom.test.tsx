// @vitest-environment jsdom
// TST-04: billing locale key consistency.
//
// Положительный: all billing_* / nav_billing keys are present in en, ru, bg with non-empty values.
// Отрицательный: a key missing in any locale or an extra key not in en causes this test to fail.
import {describe, expect, it} from 'vitest';
import en from '../../../locales/en/common.json';
import ru from '../../../locales/ru/common.json';
import bg from '../../../locales/bg/common.json';

const ALL = {en, ru, bg} as const;
type LocaleName = keyof typeof ALL;

function billingKeys(catalog: Record<string, string>): string[] {
  return Object.keys(catalog).filter(k => k === 'nav_billing' || k.startsWith('billing_'));
}

const enKeys = billingKeys(en as Record<string, string>);

describe('billing locale keys', () => {
  it('en has at least one billing key', () => {
    expect(enKeys.length).toBeGreaterThan(0);
  });

  for (const locale of ['ru', 'bg'] as LocaleName[]) {
    const catalog = ALL[locale] as Record<string, string>;

    it(`${locale}: all en billing keys present`, () => {
      const missing = enKeys.filter(k => !(k in catalog));
      expect(missing).toEqual([]);
    });

    it(`${locale}: no extra billing keys vs en`, () => {
      const extra = billingKeys(catalog).filter(k => !enKeys.includes(k));
      expect(extra).toEqual([]);
    });

    it(`${locale}: all billing values non-empty`, () => {
      const empty = enKeys.filter(k => !catalog[k] || catalog[k].trim() === '');
      expect(empty).toEqual([]);
    });
  }
});
