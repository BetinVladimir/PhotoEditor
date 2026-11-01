// @vitest-environment jsdom
// UI-04: Plan selection — catalog display, current highlighted, custom hidden, Paper-only.
import {cleanup, render, screen} from '@testing-library/react';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import en from '../../../locales/en/common.json';
import ru from '../../../locales/ru/common.json';
import bg from '../../../locales/bg/common.json';

const state = vi.hoisted(() => ({
  locale: 'en' as 'en' | 'ru' | 'bg',
  billingCatalog: vi.fn(),
}));

vi.mock('react-native', async () => import('react-native-web'));
vi.mock('./localized-router', () => ({
  useLocalSearchParams: () => ({tenant: 't1', currentPlan: 'starter'}),
  useRouter: () => ({push: vi.fn()}),
  LegacyLocaleRoute: ({children}: {children: React.ReactNode}) => <>{children}</>,
}));
vi.mock('./auth-context', () => {
  const auth = {origin: 'https://plans.test', client: () => ({})};
  return {useAuth: () => auth};
});
vi.mock('./owner-api', () => ({OwnerAPI: class {billingCatalog = state.billingCatalog;}}));
vi.mock('./locale-context', async () => {
  const __factory = () => ({useLocale: () => ({locale: state.locale, t: (key: keyof typeof en) => ({en, ru, bg})[state.locale][key]})});
  const __out = await __factory();
  const {withUILocaleDouble} = await import('./test/locale-double');
  return {...__out, useLocale: () => withUILocaleDouble(__out.useLocale())};
});
vi.mock('./use-review-foreground', () => ({useReviewForeground: () => ({active: true, generation: 0})}));

const CATALOG = {
  catalog_revision: 'r1', scope: 'public', currency: 'eur',
  items: [
    {plan_code: 'starter', name_key: 'Starter', custom: false, published: true, draft_only: false,
     price: {currency: 'eur', net_minor: '0', gross_minor: '0', tax_mode: 'bg_exclusive_20'},
     limits: [], can_purchase: true, unavailable_reason: null},
    {plan_code: 'pro', name_key: 'Pro', custom: false, published: true, draft_only: false,
     price: {currency: 'eur', net_minor: '1000', gross_minor: '1200', tax_mode: 'bg_exclusive_20'},
     limits: [], can_purchase: true, unavailable_reason: null},
    {plan_code: 'studio_pro', name_key: 'Studio Pro', custom: false, published: true, draft_only: false,
     price: {currency: 'eur', net_minor: '2000', gross_minor: '2400', tax_mode: 'bg_exclusive_20'},
     limits: [], can_purchase: false, unavailable_reason: 'gate_disabled'},
    // Custom plan must be hidden (criteria)
    {plan_code: 'custom', name_key: 'Custom', custom: true, published: true, draft_only: false,
     price: {currency: 'eur', net_minor: '0', gross_minor: '0', tax_mode: 'bg_exclusive_20'},
     limits: [], can_purchase: false, unavailable_reason: null},
  ],
};

import BillingPlans from '../screens/settings/billing-plans';

beforeEach(() => {
  vi.resetAllMocks();
  state.locale = 'en';
  state.billingCatalog.mockResolvedValue(CATALOG);
});
afterEach(cleanup);

it('shows plan list with title', async () => {
  render(<BillingPlans />);
  await screen.findByText(en.billing_plans_title);
  expect(screen.getByText('Starter')).toBeTruthy();
  expect(screen.getByText('Pro')).toBeTruthy();
});

it('hides custom plan', async () => {
  render(<BillingPlans />);
  await screen.findByText('Starter');
  expect(screen.queryByText('Custom')).toBeNull();
});

it('shows coming soon for unavailable plan', async () => {
  render(<BillingPlans />);
  await screen.findByText('Studio Pro');
  expect(screen.getByText(en.billing_plans_coming_soon)).toBeTruthy();
});

it('shows current plan label', async () => {
  render(<BillingPlans />);
  await screen.findByText(en.billing_plans_current);
});

it.each([['ru', ru], ['bg', bg]] as const)('shows title in %s', async (locale, catalog) => {
  state.locale = locale;
  render(<BillingPlans />);
  await screen.findByText(catalog.billing_plans_title);
});

it('shows error state', async () => {
  state.billingCatalog.mockRejectedValue(new Error('net'));
  render(<BillingPlans />);
  await screen.findByText(en.read_error);
});
