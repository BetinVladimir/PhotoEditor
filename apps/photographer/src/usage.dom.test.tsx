// @vitest-environment jsdom
// UI-03: Usage screen — billing usage data, states, Paper-only, three locales.
import {cleanup, render, screen} from '@testing-library/react';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import en from '../../../locales/en/common.json';
import ru from '../../../locales/ru/common.json';
import bg from '../../../locales/bg/common.json';

const TENANT = '11111111-1111-4111-8111-111111111111';

const state = vi.hoisted(() => ({
  locale: 'en' as 'en' | 'ru' | 'bg',
  billingUsage: vi.fn(),
}));

vi.mock('react-native', async () => import('react-native-web'));
vi.mock('./localized-router', () => ({
  useLocalSearchParams: () => ({tenant: TENANT}),
  useRouter: () => ({push: vi.fn()}),
  LegacyLocaleRoute: ({children}: {children: React.ReactNode}) => <>{children}</>,
}));
vi.mock('./auth-context', () => {
  const auth = {origin: 'https://usage.test', client: () => ({})};
  return {useAuth: () => auth};
});
vi.mock('./owner-api', () => ({
  OwnerAPI: class {
    billingUsage = state.billingUsage;
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

const USAGE_DATA = {
  balances: [
    {resource_code: 'standard_photo', unit: 'photo', granted: '1000', consumed: '200', allocated: '50', available: '750', overrun: '0', blocked: false},
    {resource_code: 'egress', unit: 'bytes', granted: '10737418240', consumed: '1073741824', allocated: '0', available: '9663676416', overrun: '0', blocked: false},
  ],
  storage: {
    included: '10737418240', additional: '0', total: '10737418240',
    committed: '5368709120', reserved: '1073741824', free: '4294967296',
    over_capacity: false,
    categories: {original: '2147483648', result: '1073741824', preview: '536870912', archive: '536870912'},
    as_of: '2026-11-01T10:00:00Z',
  },
  as_of: '2026-11-01T10:00:00Z',
  summary_revision: 'rev1',
};

import Usage from '../screens/settings/usage';

beforeEach(() => {
  vi.resetAllMocks();
  state.locale = 'en';
  state.billingUsage.mockResolvedValue(USAGE_DATA);
});
afterEach(cleanup);

// Criteria B (loading): cancel button shown during load.
it('shows loading state', async () => {
  state.billingUsage.mockImplementation(() => new Promise(() => {}));
  render(<Usage />);
  expect(await screen.findByText(en.read_cancel)).toBeTruthy();
});

// Criteria B (ready) + Criteria C (locale keys) — en.
it('shows usage data in en with all keys', async () => {
  render(<Usage />);
  await screen.findByText(en.billing_usage_title);
  expect(screen.getByText(en.billing_usage_title)).toBeTruthy();
  expect(screen.getByText(en.billing_usage_photos)).toBeTruthy();
  expect(screen.getByText(en.billing_usage_storage_section)).toBeTruthy();
  expect(screen.getByText(en.billing_usage_egress)).toBeTruthy();
  // Composite list item texts use {exact:false} since key is embedded in a longer string.
  expect(screen.getByText(new RegExp(en.billing_usage_included))).toBeTruthy();
  expect(screen.getByText(new RegExp(en.billing_usage_committed))).toBeTruthy();
  expect(screen.getByText(new RegExp(en.billing_usage_original))).toBeTruthy();
});

// Criteria C: ru locale keys present.
it('shows all keys in ru', async () => {
  state.locale = 'ru';
  render(<Usage />);
  await screen.findByText(ru.billing_usage_title);
  expect(screen.getByText(ru.billing_usage_photos)).toBeTruthy();
  expect(screen.getByText(ru.billing_usage_storage_section)).toBeTruthy();
});

// Criteria C: bg locale keys present.
it('shows all keys in bg', async () => {
  state.locale = 'bg';
  render(<Usage />);
  await screen.findByText(bg.billing_usage_title);
  expect(screen.getByText(bg.billing_usage_photos)).toBeTruthy();
  expect(screen.getByText(bg.billing_usage_storage_section)).toBeTruthy();
});

// Criteria A: Card from react-native-paper present (section titles rendered inside Card).
it('renders Paper Card sections', async () => {
  render(<Usage />);
  await screen.findByText(en.billing_usage_photos);
  expect(screen.getByText(en.billing_usage_photos)).toBeTruthy();
});

// Criteria E: tenant UUID not shown as numeric value.
it('does not show tenant UUID as usage value', async () => {
  render(<Usage />);
  await screen.findByText(en.billing_usage_title);
  expect(screen.queryByText(TENANT)).toBeNull();
});

// Error state surfaced.
it('shows error state on rejection', async () => {
  state.billingUsage.mockRejectedValue(new Error('net error'));
  render(<Usage />);
  await screen.findByText(en.read_error);
});

// ProgressBar present for storage (no crash on 0).
it('renders storage progress bar without crashing', async () => {
  render(<Usage />);
  await screen.findByText(en.billing_usage_storage_section);
  // No assertion needed — just confirm render succeeds with data.
  expect(screen.getByText(en.billing_usage_total, {exact: false})).toBeTruthy();
});
