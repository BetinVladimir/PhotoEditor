// @vitest-environment jsdom
// UI-11: legal-onboarding screen — locale keys, states, Paper-only.
import {cleanup, render, screen} from '@testing-library/react';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import en from '../../../locales/en/common.json';
import ru from '../../../locales/ru/common.json';
import bg from '../../../locales/bg/common.json';

const TENANT = '11111111-1111-4111-8111-111111111111';

const state = vi.hoisted(() => ({
  locale: 'en' as 'en' | 'ru' | 'bg',
  legalOnboarding: vi.fn(),
  legalAcceptAll: vi.fn(),
}));

vi.mock('react-native', async () => import('react-native-web'));
vi.mock('./localized-router', () => ({
  useLocalSearchParams: () => ({tenant: TENANT}),
  useRouter: () => ({push: vi.fn(), back: vi.fn()}),
  LegacyLocaleRoute: ({children}: {children: React.ReactNode}) => <>{children}</>,
}));
vi.mock('./auth-context', () => {
  const auth = {origin: 'https://legal.test', client: () => ({})};
  return {useAuth: () => auth};
});
vi.mock('./owner-api', () => ({
  OwnerAPI: class {
    legalOnboarding = state.legalOnboarding;
    legalAcceptAll = state.legalAcceptAll;
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

const ONBOARDING_DATA = {
  docs_requiring_acceptance: [
    {document_key: 'terms', version: '2026-01-01', url: 'https://legal.beeloy.org/terms', reacceptance_required: true},
    {document_key: 'privacy', version: '2026-01-01', url: 'https://legal.beeloy.org/privacy', reacceptance_required: true},
  ],
  acceptance_token: 'tok_test_abc',
};

import LegalOnboarding from '../screens/settings/legal-onboarding';

beforeEach(() => {
  vi.resetAllMocks();
  state.locale = 'en';
  state.legalOnboarding.mockResolvedValue(ONBOARDING_DATA);
  state.legalAcceptAll.mockResolvedValue(undefined);
});
afterEach(cleanup);

it('shows loading state initially', async () => {
  state.legalOnboarding.mockImplementation(() => new Promise(() => {}));
  render(<LegalOnboarding />);
  expect(await screen.findByText(en.read_cancel)).toBeTruthy();
});

it.each([['en', en], ['ru', ru], ['bg', bg]] as const)(
  'shows all legal-onboarding locale keys in %s',
  async (locale, catalog) => {
    state.locale = locale;
    render(<LegalOnboarding />);
    await screen.findByText(catalog.legal_onboarding_title);
    for (const key of [
      'legal_onboarding_title',
      'legal_onboarding_intro',
      'legal_onboarding_accept',
      'legal_onboarding_decline',
      'legal_onboarding_doc_terms',
      'legal_onboarding_doc_privacy',
    ] as const) {
      expect(screen.getByText(catalog[key])).toBeTruthy();
    }
  }
);

it('shows error state on rejection', async () => {
  state.legalOnboarding.mockRejectedValue(new Error('network'));
  render(<LegalOnboarding />);
  expect(await screen.findByText(en.read_error)).toBeTruthy();
});
