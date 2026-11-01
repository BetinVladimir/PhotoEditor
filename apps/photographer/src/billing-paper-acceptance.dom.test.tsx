// @vitest-environment jsdom
// UI-15: Paper acceptance — verifies actual Paper screens are used in tests,
// not HTML copies; and that Hosted Checkout on native is separately qualified.
import {cleanup, render, screen, act} from '@testing-library/react';
import {afterEach, expect, it, vi} from 'vitest';

vi.mock('react-native', async () => import('react-native-web'));
vi.mock('./use-review-foreground', () => ({useReviewForeground: () => ({active: true, generation: 0})}));

const nav = vi.hoisted(() => ({push: vi.fn(), back: vi.fn(), replace: vi.fn()}));
vi.mock('./localized-router', () => ({
  router: nav,
  useRouter: () => nav,
  useLocalSearchParams: () => ({tenant: '11111111-1111-4111-8111-111111111111'}),
  LegacyLocaleRoute: ({children}: {children: React.ReactNode}) => <>{children}</>,
}));
vi.mock('./auth-context', () => {
  const auth = {client: () => ({}), origin: 'https://test'};
  return {useAuth: () => auth};
});

// Stub all billing methods the screens call (typed or as any)
vi.mock('./owner-api', () => ({
  OwnerAPI: class {
    billingSummary = vi.fn().mockResolvedValue({
      plan: {code: 'free', name_key: 'Free', custom: false},
      entitlement_state: 'active', billing_mode: 'subscription',
      period: null, recurring_total: null, items: [], pending_change: null,
      actions: {can_upgrade: true, can_downgrade: false, can_cancel: false, can_purchase_addons: false},
    });
    billingCatalog = vi.fn().mockResolvedValue({plans: [], catalog_revision: 'r1', scope: 'global', currency: 'EUR'});
    billingAddonOffers = vi.fn().mockResolvedValue({items: []});
    billingUsage = vi.fn().mockResolvedValue({buckets: [], usage_period: {starts_at: '2026-01-01', ends_at: '2026-12-31'}});
    billingPaymentDetails = vi.fn().mockResolvedValue({method: null});
  },
}));

vi.mock('./locale-context', async () => {
  const {default: en} = await import('../../../locales/en/common.json');
  const {withUILocaleDouble} = await import('./test/locale-double');
  return {useLocale: () => withUILocaleDouble({locale: 'en', setLocale: () => {}, t: (key: keyof typeof en) => en[key]})};
});

import Billing from '../screens/settings/billing';
import BillingPlans from '../screens/settings/billing-plans';
import BillingResources from '../screens/settings/billing-resources';
import Usage from '../screens/settings/usage';
import BillingPaymentDetails, {NATIVE_PAYMENT_CAPABILITY} from '../screens/settings/billing-payment-details';

afterEach(() => {cleanup(); vi.clearAllMocks();});

// === Criterion A: DOM tests render actual Paper screens ===
it('billing.tsx renders actual Paper screen title (criterion A)', () => {
  render(<Billing/>);
  expect(screen.getByText('Billing')).toBeTruthy();
});

it('billing-plans.tsx renders actual Paper screen title (criterion A)', () => {
  render(<BillingPlans/>);
  expect(screen.getByText('Select plan')).toBeTruthy();
});

it('billing-resources.tsx renders actual Paper screen title (criterion A)', () => {
  render(<BillingResources/>);
  expect(screen.getByText('Add-on resources')).toBeTruthy();
});

it('usage.tsx renders actual Paper screen title (criterion A)', () => {
  render(<Usage/>);
  expect(screen.getByText('Usage')).toBeTruthy();
});

it('billing-payment-details.tsx renders actual Paper screen title (criterion A)', () => {
  render(<BillingPaymentDetails/>);
  expect(screen.getByText('Payment details')).toBeTruthy();
});

// === Criterion C: Hosted Checkout on native requires separate qualification ===
it('NATIVE_PAYMENT_CAPABILITY is false: web e2e does not qualify native Hosted Checkout (criterion C)', () => {
  expect(NATIVE_PAYMENT_CAPABILITY).toBe(false);
});

it('billing-payment-details has no native payment auto-trigger (criterion C)', () => {
  render(<BillingPaymentDetails/>);
  expect(screen.queryByText('Native payment')).toBeNull();
});
