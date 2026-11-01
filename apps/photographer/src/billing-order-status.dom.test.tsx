// @vitest-environment jsdom
// UI-07: billing-order-status screen — locale keys, states, Paper-only.
import {cleanup, render, screen} from '@testing-library/react';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import en from '../../../locales/en/common.json';
import ru from '../../../locales/ru/common.json';
import bg from '../../../locales/bg/common.json';

const TENANT = '11111111-1111-4111-8111-111111111111';
const ORDER_ID = 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa';

const state = vi.hoisted(() => ({
  locale: 'en' as 'en' | 'ru' | 'bg',
  billingOrder: vi.fn(),
}));

vi.mock('react-native', async () => import('react-native-web'));
vi.mock('./localized-router', () => ({
  useLocalSearchParams: () => ({tenant: TENANT, id: ORDER_ID}),
  useRouter: () => ({push: vi.fn()}),
  LegacyLocaleRoute: ({children}: {children: React.ReactNode}) => <>{children}</>,
}));
vi.mock('./auth-context', () => {
  const auth = {origin: 'https://billing.test', client: () => ({})};
  return {useAuth: () => auth};
});
vi.mock('./owner-api', () => ({
  OwnerAPI: class {
    billingOrder = state.billingOrder;
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

const PAID_ORDER = {
  order_id: ORDER_ID,
  state: 'paid',
  amount: {currency: 'eur', net_minor: '1000', gross_minor: '1200', tax_minor: '200', tax_mode: 'bg_exclusive_20'},
  created_at: '2026-11-01T10:00:00Z',
  updated_at: '2026-11-01T10:05:00Z',
  plan_code: 'pro',
};

import BillingOrderStatus from '../screens/settings/billing-order-status';

beforeEach(() => {
  vi.resetAllMocks();
  state.locale = 'en';
  state.billingOrder.mockResolvedValue(PAID_ORDER);
});
afterEach(cleanup);

it('shows loading state initially', async () => {
  state.billingOrder.mockImplementation(() => new Promise(() => {}));
  render(<BillingOrderStatus />);
  expect(await screen.findByText(en.read_cancel)).toBeTruthy();
});

// Labels appear as "Label: value" — use exact:false for label keys.
it.each([['en', en], ['ru', ru], ['bg', bg]] as const)(
  'shows all order-status locale keys in %s',
  async (locale, catalog) => {
    state.locale = locale;
    render(<BillingOrderStatus />);
    // Title is standalone.
    await screen.findByText(catalog.billing_order_title);
    // Labels with values — exact:false.
    for (const key of [
      'billing_order_status',
      'billing_order_amount',
      'billing_order_created',
      'billing_order_updated',
    ] as const) {
      expect(screen.getByText(catalog[key], {exact: false})).toBeTruthy();
    }
    // State label appears inline — exact:false.
    expect(screen.getByText(catalog.billing_order_state_paid, {exact: false})).toBeTruthy();
  }
);

it('shows error state on rejection', async () => {
  state.billingOrder.mockRejectedValue(new Error('not found'));
  render(<BillingOrderStatus />);
  expect(await screen.findByText(en.read_error)).toBeTruthy();
});

it('shows empty message when order not found', async () => {
  state.billingOrder.mockResolvedValue(null);
  render(<BillingOrderStatus />);
  expect(await screen.findByText(en.billing_order_empty)).toBeTruthy();
});

it('does not display order UUID as visible text', async () => {
  render(<BillingOrderStatus />);
  await screen.findByText(en.billing_order_title);
  // ORDER_ID should not appear as standalone price-like text
  expect(screen.queryByText(ORDER_ID)).toBeNull();
});
