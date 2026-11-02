import {formatMoney as formatBillingMoney} from '../../src/billing-format';
import {useLocale} from '../../src/locale-context';
import {Card, Text} from 'react-native-paper';
import {useMemo} from 'react';
import {useLocalSearchParams} from '../../src/localized-router';
import {SafeAreaView, View} from 'react-native';
import {useAuth} from '../../src/auth-context';
import {OwnerAPI} from '../../src/owner-api';
import {useReadResource} from '../../src/use-read-resource';
import {ReadResourceState} from '../../src/read-resource-state';
import {catalogStyle as s} from '../../src/catalog-style';

// Local DTO — mirrors billing order endpoint (DTO 09 §7).
// Criteria E: price and tenant never come from route params.
type OrderMoney = {currency: string; net_minor: string; gross_minor: string; tax_minor: string; tax_mode: string};
type BillingOrder = {
  order_id: string;
  state: string;
  amount: OrderMoney | null;
  created_at: string;
  updated_at: string;
  plan_code: string | null;
};

const STATE_KEYS: Record<string, keyof ReturnType<ReturnType<typeof useLocale>['t']> extends never ? string : never> = {};
void STATE_KEYS; // unused — state label mapped inline

export default function BillingOrderStatus() {
  const {t,locale} = useLocale();
  // id is the order ID used as API parameter only (Criteria E).
  const {tenant, id} = useLocalSearchParams<{tenant: string; id: string}>();
  const auth = useAuth();
  const api = useMemo(() => new OwnerAPI(auth.client(), tenant || ''), [auth, tenant]);

  const resource = useReadResource(
    JSON.stringify([auth.origin, tenant, 'billing-order', id]),
    api,
    async signal => {
      if (!tenant || !id) throw new Error('Workspace and order ID required');
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      return (api as any).billingOrder(id, signal) as Promise<BillingOrder>;
    },
  );

  const data = resource.data;

  const stateLabel = (state: string) => {
    const map: Record<string, string> = {
      pending: t('billing_order_state_pending'),
      paid: t('billing_order_state_paid'),
      failed: t('billing_order_state_failed'),
      cancelled: t('billing_order_state_cancelled'),
      refunded: t('billing_order_state_refunded'),
    };
    return map[state] ?? state;
  };

  const formatMoney = (m: OrderMoney | null | undefined) => {
    if (!m) return '';
    return `${formatBillingMoney(m.gross_minor,m.currency,locale)}`;
  };

  return (
    <SafeAreaView style={s.root}>
      <View style={s.header}>
        <Text variant="headlineMedium" style={s.title}>{t('billing_order_title')}</Text>
        <ReadResourceState phase={resource.phase} retry={resource.retry} cancel={resource.cancel} />
        {!data && resource.phase === 'ready' && (
          <Text style={s.empty}>{t('billing_order_empty')}</Text>
        )}
        {data && (
          <Card style={{marginBottom: 8}}>
            <Card.Content>
              <Text style={s.meta}>{t('billing_order_status')}: {stateLabel(data.state)}</Text>
              {data.amount && (
                <Text style={s.meta}>{t('billing_order_amount')}: {formatMoney(data.amount)}</Text>
              )}
              <Text style={s.meta}>{t('billing_order_created')}: {data.created_at}</Text>
              <Text style={s.meta}>{t('billing_order_updated')}: {data.updated_at}</Text>
            </Card.Content>
          </Card>
        )}
      </View>
    </SafeAreaView>
  );
}
