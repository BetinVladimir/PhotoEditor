// @vitest-environment jsdom
// UI-10: privacy screen — locale keys, states, Paper-only.
import {cleanup, render, screen} from '@testing-library/react';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import en from '../../../locales/en/common.json';
import ru from '../../../locales/ru/common.json';
import bg from '../../../locales/bg/common.json';

const TENANT = '11111111-1111-4111-8111-111111111111';

const state = vi.hoisted(() => ({
  locale: 'en' as 'en' | 'ru' | 'bg',
  privacySettings: vi.fn(),
}));

vi.mock('react-native', async () => import('react-native-web'));
vi.mock('./localized-router', () => ({
  useLocalSearchParams: () => ({tenant: TENANT}),
  useRouter: () => ({push: vi.fn()}),
  LegacyLocaleRoute: ({children}: {children: React.ReactNode}) => <>{children}</>,
}));
vi.mock('./auth-context', () => {
  const auth = {origin: 'https://privacy.test', client: () => ({})};
  return {useAuth: () => auth};
});
vi.mock('./owner-api', () => ({
  OwnerAPI: class {
    privacySettings = state.privacySettings;
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

const PRIVACY_DATA = {
  legal_docs: [
    {document_key: 'terms', locale: 'en', version: '2026-01-01', url: 'https://legal.beeloy.org/terms/en/2026-01-01'},
    {document_key: 'privacy', locale: 'en', version: '2026-01-01', url: 'https://legal.beeloy.org/privacy/en/2026-01-01'},
  ],
  can_request_erasure: true,
  can_download_data: true,
};

import Privacy from '../screens/settings/privacy';

beforeEach(() => {
  vi.resetAllMocks();
  state.locale = 'en';
  state.privacySettings.mockResolvedValue(PRIVACY_DATA);
});
afterEach(cleanup);

it('shows loading state initially', async () => {
  state.privacySettings.mockImplementation(() => new Promise(() => {}));
  render(<Privacy />);
  expect(await screen.findByText(en.read_cancel)).toBeTruthy();
});

it.each([['en', en], ['ru', ru], ['bg', bg]] as const)(
  'shows all privacy locale keys in %s',
  async (locale, catalog) => {
    state.locale = locale;
    render(<Privacy />);
    await screen.findByText(catalog.privacy_title);
    for (const key of [
      'privacy_title',
      'privacy_data_section',
      'privacy_erasure_section',
      'privacy_legal_docs',
      'privacy_doc_terms',
      'privacy_doc_privacy',
      'privacy_request_erasure',
      'privacy_download_data',
    ] as const) {
      expect(screen.getByText(catalog[key])).toBeTruthy();
    }
  }
);

it('shows error state on rejection', async () => {
  state.privacySettings.mockRejectedValue(new Error('network'));
  render(<Privacy />);
  expect(await screen.findByText(en.read_error)).toBeTruthy();
});

it('shows empty message when no settings', async () => {
  state.privacySettings.mockResolvedValue(null);
  render(<Privacy />);
  expect(await screen.findByText(en.privacy_empty)).toBeTruthy();
});
