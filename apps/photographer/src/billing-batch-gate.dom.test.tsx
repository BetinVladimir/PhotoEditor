// @vitest-environment jsdom
// UI-09: billing-batch-gate dialog — locale keys, Paper-only, no own route.
import {cleanup, render, screen, fireEvent} from '@testing-library/react';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import {PaperProvider} from 'react-native-paper';
import en from '../../../locales/en/common.json';
import ru from '../../../locales/ru/common.json';
import bg from '../../../locales/bg/common.json';

const state = vi.hoisted(() => ({
  locale: 'en' as 'en' | 'ru' | 'bg',
}));

vi.mock('react-native', async () => import('react-native-web'));
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

import BillingBatchGate from '../screens/settings/billing-batch-gate';

const onProceed = vi.fn();
const onDismiss = vi.fn();

const renderGate = (props?: Partial<React.ComponentProps<typeof BillingBatchGate>>) =>
  render(
    <PaperProvider>
      <BillingBatchGate
        visible={true}
        available="50"
        required="100"
        onProceed={onProceed}
        onDismiss={onDismiss}
        {...props}
      />
    </PaperProvider>
  );

beforeEach(() => {
  vi.resetAllMocks();
  state.locale = 'en';
});
afterEach(cleanup);

it.each([['en', en], ['ru', ru], ['bg', bg]] as const)(
  'shows all batch-gate locale keys in %s',
  (locale, catalog) => {
    state.locale = locale;
    renderGate();
    for (const key of [
      'billing_batch_gate_title',
      'billing_batch_gate_message',
      'billing_batch_gate_available',
      'billing_batch_gate_required',
      'billing_batch_gate_proceed',
      'billing_batch_gate_dismiss',
    ] as const) {
      expect(screen.getAllByText(catalog[key], {exact: false}).length).toBeGreaterThan(0);
    }
  }
);

it('calls onProceed when proceed button is pressed', () => {
  renderGate();
  fireEvent.click(screen.getByText(en.billing_batch_gate_proceed));
  expect(onProceed).toHaveBeenCalledOnce();
});

it('calls onDismiss when dismiss button is pressed', () => {
  renderGate();
  fireEvent.click(screen.getByText(en.billing_batch_gate_dismiss));
  expect(onDismiss).toHaveBeenCalledOnce();
});

it('shows available and required quantities', () => {
  renderGate({available: '42', required: '999'});
  expect(screen.getByText(`${en.billing_batch_gate_available}: 42`)).toBeTruthy();
  expect(screen.getByText(`${en.billing_batch_gate_required}: 999`)).toBeTruthy();
});
