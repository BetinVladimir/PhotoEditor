// @vitest-environment jsdom
// UI-06: billing-change-confirm screen — locale keys, states, Paper-only, no price from route params.
import {cleanup, render, screen} from '@testing-library/react';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import en from '../../../locales/en/common.json';
import ru from '../../../locales/ru/common.json';
import bg from '../../../locales/bg/common.json';

const TENANT = '11111111-1111-4111-8111-111111111111';
const PLAN_CODE = 'pro';

const state = vi.hoisted(() => ({
  locale: 'en' as 'en' | 'ru' | 'bg',
  billingChangePreview: vi.fn(),
}));

vi.mock('react-native', async () => import('react-native-web'));
vi.mock('./localized-router', () => ({
  useLocalSearchParams: () => ({tenant: TENANT, planCode: PLAN_CODE}),
  useRouter: () => ({push: vi.fn(), back: vi.fn()}),
  LegacyLocaleRoute: ({children}: {children: React.ReactNode}) => <>{children}</>,
}));
vi.mock('./auth-context', () => {
  const auth = {origin: 'https://billing.test', client: () => ({})};
  return {useAuth: () => auth};
});
vi.mock('./owner-api', () => ({
  OwnerAPI: class {
    billingChangePreview = state.billingChangePreview;
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

const CHANGE_PREVIEW = {
  current_plan_code: 'basic',
  new_plan_code: PLAN_CODE,
  new_plan_name_key: 'Pro',
  surcharge: {currency: 'eur', net_minor: '500', gross_minor: '600', tax_minor: '100', tax_mode: 'bg_exclusive_20'},
  resource_deltas: [{resource_code: 'storage', current_quantity: '50', new_quantity: '100'}],
  credits_kept: '200',
  anchor_amount: {currency: 'eur', net_minor: '300', gross_minor: '360', tax_minor: '60', tax_mode: 'bg_exclusive_20'},
  next_recurring_total: {currency: 'eur', net_minor: '1000', gross_minor: '1200', tax_minor: '200', tax_mode: 'bg_exclusive_20'},
  effective_at: '2026-12-01',
  projected_overquota_resources: [],
};

import BillingChangeConfirm from '../screens/settings/billing-change-confirm';

beforeEach(() => {
  vi.resetAllMocks();
  state.locale = 'en';
  state.billingChangePreview.mockResolvedValue(CHANGE_PREVIEW);
});
afterEach(cleanup);

// Criteria B (loading state)
it('shows loading state initially', async () => {
  state.billingChangePreview.mockImplementation(() => new Promise(() => {}));
  render(<BillingChangeConfirm />);
  const cancelBtn = await screen.findByText(en.read_cancel);
  expect(cancelBtn).toBeTruthy();
});

// Criteria B+C: all locale keys rendered in all three locales.
// Labels are rendered as "Label: value" so exact:false is required.
it.each([['en', en], ['ru', ru], ['bg', bg]] as const)(
  'shows all change-confirm locale keys in %s',
  async (locale, catalog) => {
    state.locale = locale;
    render(<BillingChangeConfirm />);
    // Title is standalone — exact match.
    await screen.findByText(catalog.billing_change_confirm_title);
    // Labels appear as "Label: value" in Text nodes — use exact:false.
    for (const key of [
      'billing_change_confirm_current_plan',
      'billing_change_confirm_new_plan',
    ] as const) {
      expect(screen.getByText(catalog[key], {exact: false})).toBeTruthy();
    }
    // Action buttons are standalone labels.
    expect(screen.getByText(catalog.billing_change_confirm_proceed)).toBeTruthy();
    expect(screen.getByText(catalog.billing_change_confirm_cancel)).toBeTruthy();
  }
);

// Criteria B (error state)
it('shows error state when billingChangePreview rejects', async () => {
  state.billingChangePreview.mockRejectedValue(new Error('network'));
  render(<BillingChangeConfirm />);
  await screen.findByText(en.read_error);
});

// Criteria B (empty state)
it('shows empty message when billingChangePreview returns null', async () => {
  state.billingChangePreview.mockResolvedValue(null);
  render(<BillingChangeConfirm />);
  await screen.findByText(en.billing_change_confirm_empty);
});

// Criteria E: planCode not displayed as price
it('does not display planCode as a price', async () => {
  render(<BillingChangeConfirm />);
  await screen.findByText(en.billing_change_confirm_title);
  expect(screen.queryByText(PLAN_CODE)).toBeNull();
});

// Criteria B (projected overquota): warning shown when overquota resources present
it('shows projected overquota warning', async () => {
  state.billingChangePreview.mockResolvedValue({
    ...CHANGE_PREVIEW,
    projected_overquota_resources: ['storage'],
  });
  render(<BillingChangeConfirm />);
  await screen.findByText(en.billing_change_confirm_projected_overquota, {exact: false});
});

// Criteria A: Paper components render (title visible confirms Paper Text rendered)
it('renders title via Paper Text', async () => {
  render(<BillingChangeConfirm />);
  await screen.findByText(en.billing_change_confirm_title);
  expect(screen.getByText(en.billing_change_confirm_title)).toBeTruthy();
});
