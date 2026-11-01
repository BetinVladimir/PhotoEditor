// @vitest-environment jsdom
// UI-05: Add-on resources — offer list, quote display, Paper-only, three locales.
import {cleanup, render, screen} from '@testing-library/react';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import en from '../../../locales/en/common.json';
import ru from '../../../locales/ru/common.json';
import bg from '../../../locales/bg/common.json';

const state = vi.hoisted(() => ({
  locale: 'en' as 'en' | 'ru' | 'bg',
  billingAddonOffers: vi.fn(),
}));

vi.mock('react-native', async () => import('react-native-web'));
vi.mock('./localized-router', () => ({
  useLocalSearchParams: () => ({tenant: 't1'}),
  useRouter: () => ({push: vi.fn()}),
  LegacyLocaleRoute: ({children}: {children: React.ReactNode}) => <>{children}</>,
}));
vi.mock('./auth-context', () => {
  const auth = {origin: 'https://resources.test', client: () => ({})};
  return {useAuth: () => auth};
});
vi.mock('./owner-api', () => ({OwnerAPI: class {billingAddonOffers = state.billingAddonOffers;}}));
vi.mock('./locale-context', async () => {
  const __factory = () => ({useLocale: () => ({locale: state.locale, t: (key: keyof typeof en) => ({en, ru, bg})[state.locale][key]})});
  const __out = await __factory();
  const {withUILocaleDouble} = await import('./test/locale-double');
  return {...__out, useLocale: () => withUILocaleDouble(__out.useLocale())};
});
vi.mock('./use-review-foreground', () => ({useReviewForeground: () => ({active: true, generation: 0})}));

const ADDON_DATA = {
  offers: [
    {offer_code: 'photo_100', resource_code: 'standard_photo', recurring: false,
     quantity_per_pack: '100', max_packs: 10,
     price_per_pack: {currency: 'eur', net_minor: '500', gross_minor: '600', tax_minor: '100'},
     expiry_days: 30},
    {offer_code: 'storage_10gb', resource_code: 'storage', recurring: true,
     quantity_per_pack: '10737418240', max_packs: 5,
     price_per_pack: {currency: 'eur', net_minor: '200', gross_minor: '240', tax_minor: '40'},
     expiry_days: null},
  ],
  subtotal: {currency: 'eur', net_minor: '700', gross_minor: '840', tax_minor: '140'},
  tax: {currency: 'eur', net_minor: '0', gross_minor: '140', tax_minor: '140'},
  total: {currency: 'eur', net_minor: '700', gross_minor: '840', tax_minor: '140'},
  valid_until: '2026-11-01T12:00:00Z',
};

import BillingResources from '../screens/settings/billing-resources';

beforeEach(() => {
  vi.resetAllMocks();
  state.locale = 'en';
  state.billingAddonOffers.mockResolvedValue(ADDON_DATA);
});
afterEach(cleanup);

it('shows addon title and offers', async () => {
  render(<BillingResources />);
  await screen.findByText(en.billing_addon_title);
  expect(screen.getByText('standard_photo')).toBeTruthy();
  expect(screen.getByText('storage')).toBeTruthy();
});

it('shows recurring note for storage offer', async () => {
  render(<BillingResources />);
  await screen.findByText('storage');
  expect(screen.getByText(new RegExp(en.billing_addon_recurring_note))).toBeTruthy();
});

it('shows subtotal and total', async () => {
  render(<BillingResources />);
  await screen.findByText(new RegExp(en.billing_addon_subtotal));
  expect(screen.getByText(new RegExp(en.billing_addon_total))).toBeTruthy();
});

it.each([['ru', ru], ['bg', bg]] as const)('shows title in %s', async (locale, catalog) => {
  state.locale = locale;
  render(<BillingResources />);
  await screen.findByText(catalog.billing_addon_title);
});

it('shows loading state', async () => {
  state.billingAddonOffers.mockImplementation(() => new Promise(() => {}));
  render(<BillingResources />);
  expect(await screen.findByText(en.read_cancel)).toBeTruthy();
});

it('shows error state', async () => {
  state.billingAddonOffers.mockRejectedValue(new Error('net'));
  render(<BillingResources />);
  await screen.findByText(en.read_error);
});
