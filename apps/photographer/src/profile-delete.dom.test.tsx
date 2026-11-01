// @vitest-environment jsdom
// UI-12: profile-delete screen — locale keys, confirm guard, Paper-only.
import {cleanup, render, screen, fireEvent} from '@testing-library/react';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import en from '../../../locales/en/common.json';
import ru from '../../../locales/ru/common.json';
import bg from '../../../locales/bg/common.json';

const TENANT = '11111111-1111-4111-8111-111111111111';

const state = vi.hoisted(() => ({
  locale: 'en' as 'en' | 'ru' | 'bg',
  profileDelete: vi.fn(),
}));

vi.mock('react-native', async () => import('react-native-web'));
vi.mock('./localized-router', () => ({
  useLocalSearchParams: () => ({tenant: TENANT}),
  useRouter: () => ({push: vi.fn(), back: vi.fn(), replace: vi.fn()}),
  LegacyLocaleRoute: ({children}: {children: React.ReactNode}) => <>{children}</>,
}));
vi.mock('./auth-context', () => {
  const auth = {origin: 'https://profile.test', client: () => ({})};
  return {useAuth: () => auth};
});
vi.mock('./owner-api', () => ({
  OwnerAPI: class {
    profileDelete = state.profileDelete;
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

import ProfileDelete from '../screens/settings/profile-delete';

beforeEach(() => {
  vi.resetAllMocks();
  state.locale = 'en';
  state.profileDelete.mockResolvedValue(undefined);
});
afterEach(cleanup);

it.each([['en', en], ['ru', ru], ['bg', bg]] as const)(
  'shows all profile-delete locale keys in %s',
  (locale, catalog) => {
    state.locale = locale;
    render(<ProfileDelete />);
    // TextInput label appears multiple times (floating + static) — use getAllByText.
    for (const key of [
      'profile_delete_title',
      'profile_delete_warning',
      'profile_delete_consequences',
      'profile_delete_photos',
      'profile_delete_account',
      'profile_delete_proceed',
      'profile_delete_cancel',
    ] as const) {
      expect(screen.getAllByText(catalog[key]).length).toBeGreaterThan(0);
    }
    // confirm_label appears via TextInput (may be duplicated) — check at least one.
    expect(screen.getAllByText(catalog.profile_delete_confirm_label).length).toBeGreaterThan(0);
  }
);

it('proceed button is disabled before correct sentinel is typed', () => {
  render(<ProfileDelete />);
  const btn = screen.getByText(en.profile_delete_proceed);
  const btnEl = btn.closest('[aria-disabled="true"]') ?? btn.closest('[disabled]');
  expect(btnEl).toBeTruthy();
});

it('proceed button enables after typing DELETE', () => {
  render(<ProfileDelete />);
  const input = screen.getByPlaceholderText(en.profile_delete_confirm_placeholder);
  fireEvent.change(input, {target: {value: 'DELETE'}});
  const btn = screen.getByText(en.profile_delete_proceed);
  const disabled = btn.closest('[aria-disabled="true"]') ?? btn.closest('[disabled]');
  expect(disabled).toBeFalsy();
});

it('proceed button stays disabled with partial input', () => {
  render(<ProfileDelete />);
  const input = screen.getByPlaceholderText(en.profile_delete_confirm_placeholder);
  fireEvent.change(input, {target: {value: 'DELET'}});
  const btn = screen.getByText(en.profile_delete_proceed);
  const btnEl = btn.closest('[aria-disabled="true"]') ?? btn.closest('[disabled]');
  expect(btnEl).toBeTruthy();
});
