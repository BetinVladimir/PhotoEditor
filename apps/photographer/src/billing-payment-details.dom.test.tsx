// @vitest-environment jsdom
// UI-08: billing-payment-details screen — locale keys, states, Paper-only.
import {cleanup, render, screen} from '@testing-library/react';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import en from '../../../locales/en/common.json';
import ru from '../../../locales/ru/common.json';
import bg from '../../../locales/bg/common.json';

const TENANT = '11111111-1111-4111-8111-111111111111';

const state = vi.hoisted(() => ({
  locale: 'en' as 'en' | 'ru' | 'bg',
  billingPaymentDetails: vi.fn(),
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
    billingPaymentDetails = state.billingPaymentDetails;
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

const WITH_CARD = {
  method: {type: 'card', brand: 'visa', last4: '4242', exp_month: 11, exp_year: 2028},
};
const NO_METHOD = {method: null};

import BillingPaymentDetails from '../screens/settings/billing-payment-details';

beforeEach(() => {
  vi.resetAllMocks();
  state.locale = 'en';
  state.billingPaymentDetails.mockResolvedValue(WITH_CARD);
});
afterEach(cleanup);

it('shows loading state initially', async () => {
  state.billingPaymentDetails.mockImplementation(() => new Promise(() => {}));
  render(<BillingPaymentDetails />);
  expect(await screen.findByText(en.read_cancel)).toBeTruthy();
});

// Labels appear inline with values — use exact:false.
it.each([['en', en], ['ru', ru], ['bg', bg]] as const)(
  'shows all payment-details locale keys in %s',
  async (locale, catalog) => {
    state.locale = locale;
    render(<BillingPaymentDetails />);
    await screen.findByText(catalog.billing_payment_title);
    // Standalone title and button.
    expect(screen.getByText(catalog.billing_payment_title)).toBeTruthy();
    expect(screen.getByText(catalog.billing_payment_manage)).toBeTruthy();
    // Labels appear inside "Label: value" text — use getAllByText (may appear multiple times).
    for (const key of [
      'billing_payment_method',
      'billing_payment_card_brand',
      'billing_payment_card_last4',
      'billing_payment_card_expiry',
    ] as const) {
      expect(screen.getAllByText(catalog[key], {exact: false}).length).toBeGreaterThan(0);
    }
  }
);

it('shows no-method message when card absent', async () => {
  state.billingPaymentDetails.mockResolvedValue(NO_METHOD);
  render(<BillingPaymentDetails />);
  expect(await screen.findByText(en.billing_payment_no_method)).toBeTruthy();
});

it('shows error state on rejection', async () => {
  state.billingPaymentDetails.mockRejectedValue(new Error('network'));
  render(<BillingPaymentDetails />);
  expect(await screen.findByText(en.read_error)).toBeTruthy();
});
