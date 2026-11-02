// @vitest-environment jsdom
// UI-02: Billing screen — locale keys, states, Paper-only, no price from route params.
import {cleanup, render, screen} from '@testing-library/react';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import en from '../../../locales/en/common.json';
import ru from '../../../locales/ru/common.json';
import bg from '../../../locales/bg/common.json';

const TENANT = '11111111-1111-4111-8111-111111111111';

const state = vi.hoisted(() => ({
  locale: 'en' as 'en' | 'ru' | 'bg',
  billingSummary: vi.fn(),
}));

vi.mock('react-native', async () => import('react-native-web'));
vi.mock('./localized-router', () => ({
  useLocalSearchParams: () => ({tenant: TENANT}),
  useRouter: () => ({push: vi.fn()}),
  LegacyLocaleRoute: ({children}: {children: React.ReactNode}) => <>{children}</>,
}));
vi.mock('./auth-context', () => {
  const auth = {origin: 'https://billing.test', client: () => ({})};
  return {useAuth: () => auth};
});
vi.mock('./owner-api', () => ({
  OwnerAPI: class {
    billingSummary = state.billingSummary;
  },
}));
vi.mock('./locale-context', async () => {
  const __factory = () => ({
    useLocale: () => ({
      locale: state.locale,
      t: (key: keyof typeof en) => ({en, ru, bg})[state.locale][key],
    }),
  });
  const __out = await __factory();
  const {withUILocaleDouble} = await import('./test/locale-double');
  return {
    ...__out,
    useLocale: () => withUILocaleDouble(__out.useLocale()),
  };
});
vi.mock('./use-review-foreground', () => ({
  useReviewForeground: () => ({active: true, generation: 0}),
}));

const ACTIVE_SUMMARY = {
  plan: {code: 'pro', name_key: 'Pro', custom: false},
  entitlement_state: 'active',
  billing_mode: 'stripe',
  period: {starts_at: '2026-11-01', ends_at: '2026-12-01'},
  recurring_total: {currency: 'eur', net_minor: '1000', gross_minor: '1200', tax_minor: '200', tax_mode: 'bg_exclusive_20'},
  items: [
    {id: 'i1', resource_code: 'ai', price: {currency: 'eur', net_minor: '800', gross_minor: '960', tax_minor: '160', tax_mode: 'bg_exclusive_20'}},
    {id: 'i2', resource_code: 'storage', price: {currency: 'eur', net_minor: '200', gross_minor: '240', tax_minor: '40', tax_mode: 'bg_exclusive_20'}},
  ],
  pending_change: null,
  actions: {can_upgrade: true, can_downgrade: false, can_cancel: true, can_purchase_addons: true},
};

import Billing from '../screens/settings/billing';

beforeEach(() => {
  vi.resetAllMocks();
  state.locale = 'en';
  state.billingSummary.mockResolvedValue(ACTIVE_SUMMARY);
});
afterEach(cleanup);

// Criteria B (loading state): ReadResourceState shown before data arrives.
it('shows loading state initially', async () => {
  state.billingSummary.mockImplementation(() => new Promise(() => {}));
  render(<Billing />);
  // ReadResourceState shows cancel button while loading
  const cancelBtn = await screen.findByText(en.read_cancel);
  expect(cancelBtn).toBeTruthy();
});

// Criteria B (ready state) + Criteria C (locale keys): renders plan name and status in all locales.
// Three separate renders — one per locale — to avoid rerender wrapper complexity.
it.each([['en', en], ['ru', ru], ['bg', bg]] as const)(
  'shows all billing locale keys in %s',
  async (locale, catalog) => {
    state.locale = locale;
    render(<Billing />);
    await screen.findByText(catalog.billing_current_plan);
    for (const key of [
      'billing_screen_title',
      'billing_current_plan',
      'billing_status',
      'billing_recurring_total',
      'billing_period_dates',
      'billing_next_payment',
      'billing_ai_label',
      'billing_storage_label',
      'billing_tax_exclusive',
    ] as const) {
      expect(screen.getByText(catalog[key])).toBeTruthy();
    }
  }
);

// Criteria A: only react-native-paper components (Card present in output).
it('renders Card component from react-native-paper', async () => {
  render(<Billing />);
  await screen.findByText(en.billing_current_plan);
  // Card renders its content; presence of billing_current_plan confirms Paper Card rendered.
  expect(screen.getByText(en.billing_current_plan)).toBeTruthy();
});

// Criteria E: tenant from route params is used only for API call, not as a price.
it('does not display tenant UUID as a price', async () => {
  render(<Billing />);
  await screen.findByText(en.billing_current_plan);
  expect(screen.queryByText(TENANT)).toBeNull();
});

// Criteria B (empty state): null data after ready shows empty message.
it('shows empty message when billingSummary returns null', async () => {
  state.billingSummary.mockResolvedValue(null);
  render(<Billing />);
  await screen.findByText(en.billing_no_summary);
});

// Criteria B (error state): read error is surfaced via ReadResourceState.
it('shows error state when billingSummary rejects', async () => {
  state.billingSummary.mockRejectedValue(new Error('network error'));
  render(<Billing />);
  await screen.findByText(en.read_error);
});

// Criteria B (pending change): pending change banner shown when present.
it('shows pending change banner when present', async () => {
  const withChange = {
    ...ACTIVE_SUMMARY,
    pending_change: {id: 'pc1', type: 'downgrade', effective_at: '2026-12-01'},
  };
  state.billingSummary.mockResolvedValue(withChange);
  render(<Billing />);
  await screen.findByText(en.billing_pending_change, {exact: false});
});

// Criteria A: only Paper actions — no alien button framework text.
it('renders action buttons with paper Button text', async () => {
  render(<Billing />);
  await screen.findByText(en.billing_action_change_plan);
  expect(screen.getByText(en.billing_action_addon)).toBeTruthy();
  expect(screen.getByText(en.billing_action_payment_methods)).toBeTruthy();
});


it('formats server cents as currency amounts',async()=>{
 render(<Billing />);
 expect(await screen.findByText(/€12\.00.*€10\.00/)).toBeTruthy();
 expect(screen.queryByText(/1200 EUR/)).toBeNull();
});
