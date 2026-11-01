import {useLocale} from '../../src/locale-context';
import {Banner, Button, Card, Text} from 'react-native-paper';
import {useMemo} from 'react';
import {useLocalSearchParams, useRouter} from '../../src/localized-router';
import {SafeAreaView, View} from 'react-native';
import {useAuth} from '../../src/auth-context';
import {OwnerAPI} from '../../src/owner-api';
import {useReadResource} from '../../src/use-read-resource';
import {ReadResourceState} from '../../src/read-resource-state';
import {catalogStyle as s} from '../../src/catalog-style';

// Local DTO — mirrors backend/internal/billing/api.go Summary.
// The endpoint does not exist yet (billing api group); working against shapes
// from DTO 09 §7. Criteria E: prices never come from route params.
type BillingMoney = {currency: string; net_minor: string; gross_minor: string; tax_minor: string; tax_mode: string};
type BillingPendingChange = {id: string; type: string; effective_at: string};
type BillingSummaryActions = {can_upgrade: boolean; can_downgrade: boolean; can_cancel: boolean; can_purchase_addons: boolean};
type BillingSummary = {
  plan: {code: string; name_key: string; custom: boolean};
  entitlement_state: string;
  billing_mode: string;
  period: {starts_at: string; ends_at: string} | null;
  recurring_total: BillingMoney | null;
  items: {id: string; resource_code: string; price: BillingMoney}[];
  pending_change: BillingPendingChange | null;
  actions: BillingSummaryActions;
};

export default function Billing() {
  const {t} = useLocale();
  const {tenant} = useLocalSearchParams<{tenant: string}>();
  const auth = useAuth();
  const api = useMemo(() => new OwnerAPI(auth.client(), tenant || ''), [auth, tenant]);

  const resource = useReadResource(
    JSON.stringify([auth.origin, tenant, 'billing-summary']),
    api,
    async signal => {
      if (!tenant) throw new Error('Workspace required');
      // Criteria E: price is fetched from server — not from route params.
      return api.billingSummary(signal) as Promise<BillingSummary>;
    },
  );

  const data = resource.data;

  const router = useRouter();
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const handleChangePlan = () => (router.push as any)({pathname: '/settings/billing/plans', params: {tenant}});
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const handleAddon = () => (router.push as any)({pathname: '/settings/billing/resources', params: {tenant}});
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const handlePaymentMethods = () => (router.push as any)({pathname: '/settings/billing/portal', params: {tenant}});

  const aiItem = data?.items.find(i => i.resource_code === 'ai');
  const storageItem = data?.items.find(i => i.resource_code === 'storage');

  const formatMoney = (m: BillingMoney | null | undefined) => {
    if (!m) return '';
    return `${m.gross_minor} ${m.currency.toUpperCase()} (${m.net_minor} ${t('billing_tax_net')})`;
  };

  return (
    <SafeAreaView style={s.root}>
      <View style={s.header}>
        <Text variant="headlineMedium" style={s.title}>{t('billing_screen_title')}</Text>
        <ReadResourceState phase={resource.phase} retry={resource.retry} cancel={resource.cancel} />
        {!data && resource.phase === 'ready' && (
          <Text style={s.empty}>{t('billing_no_summary')}</Text>
        )}
        {data && (
          <>
            <Card>
              <Card.Content>
                <Text variant="titleMedium" style={s.name}>{t('billing_current_plan')}</Text>
                <Text style={s.subtitle}>
                  {data.plan.custom ? t('billing_plan_custom_label') : data.plan.name_key}
                </Text>
                <Text variant="titleMedium" style={s.name}>{t('billing_status')}</Text>
                <Text style={s.subtitle}>{data.entitlement_state}</Text>
                {data.recurring_total && (
                  <>
                    <Text variant="titleMedium" style={s.name}>{t('billing_recurring_total')}</Text>
                    <Text style={s.subtitle}>{formatMoney(data.recurring_total)}</Text>
                    <Text style={s.meta}>{t('billing_tax_exclusive')}</Text>
                  </>
                )}
                {data.period && (
                  <>
                    <Text variant="titleMedium" style={s.name}>{t('billing_period_dates')}</Text>
                    <Text style={s.subtitle}>
                      {data.period.starts_at} — {data.period.ends_at}
                    </Text>
                    <Text variant="titleMedium" style={s.name}>{t('billing_next_payment')}</Text>
                    <Text style={s.subtitle}>{data.period.ends_at}</Text>
                  </>
                )}
                {aiItem && (
                  <>
                    <Text variant="titleMedium" style={s.name}>{t('billing_ai_label')}</Text>
                    <Text style={s.meta}>{formatMoney(aiItem.price)}</Text>
                  </>
                )}
                {storageItem && (
                  <>
                    <Text variant="titleMedium" style={s.name}>{t('billing_storage_label')}</Text>
                    <Text style={s.meta}>{formatMoney(storageItem.price)}</Text>
                  </>
                )}
                {data.pending_change && (
                  <Banner visible icon="information" actions={[]}>
                    {`${t('billing_pending_change')}: ${data.pending_change.type} — ${data.pending_change.effective_at}`}
                  </Banner>
                )}
              </Card.Content>
              <Card.Actions>
                {data.actions.can_upgrade || data.actions.can_downgrade ? (
                  <Button mode="contained" onPress={handleChangePlan}>{t('billing_action_change_plan')}</Button>
                ) : null}
                {data.actions.can_purchase_addons && (
                  <Button mode="outlined" onPress={handleAddon}>{t('billing_action_addon')}</Button>
                )}
                <Button mode="text" onPress={handlePaymentMethods}>{t('billing_action_payment_methods')}</Button>
              </Card.Actions>
            </Card>
          </>
        )}
      </View>
    </SafeAreaView>
  );
}
