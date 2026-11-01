import {useLocale} from '../../src/locale-context';
import {Button, Card, Text} from 'react-native-paper';
import {useMemo} from 'react';
import {useLocalSearchParams, useRouter} from '../../src/localized-router';
import {SafeAreaView, View} from 'react-native';
import {useAuth} from '../../src/auth-context';
import {OwnerAPI} from '../../src/owner-api';
import {useReadResource} from '../../src/use-read-resource';
import {ReadResourceState} from '../../src/read-resource-state';
import {catalogStyle as s} from '../../src/catalog-style';

// Local DTO — mirrors billing payment method endpoint (DTO 09 §7).
type PaymentMethod = {
  type: string;
  brand: string | null;
  last4: string | null;
  exp_month: number | null;
  exp_year: number | null;
};
type PaymentDetails = {method: PaymentMethod | null};

/** Native payment channel is not auto-enabled (R14). Capability requires separate store qualification. */
export const NATIVE_PAYMENT_CAPABILITY = false as const;

export default function BillingPaymentDetails() {
  const {t} = useLocale();
  const {tenant} = useLocalSearchParams<{tenant: string}>();
  const auth = useAuth();
  const api = useMemo(() => new OwnerAPI(auth.client(), tenant || ''), [auth, tenant]);

  const resource = useReadResource(
    JSON.stringify([auth.origin, tenant, 'billing-payment-details']),
    api,
    async signal => {
      if (!tenant) throw new Error('Workspace required');
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      return (api as any).billingPaymentDetails(signal) as Promise<PaymentDetails>;
    },
  );

  const data = resource.data;
  const router = useRouter();
  const handleManage = () => {
    // Opens Stripe Customer Portal (external page) — R13.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (router.push as any)({pathname: '/settings/billing/portal', params: {tenant}});
  };

  const formatExpiry = (month: number | null, year: number | null) => {
    if (!month || !year) return '';
    return `${String(month).padStart(2, '0')}/${year}`;
  };

  return (
    <SafeAreaView style={s.root}>
      <View style={s.header}>
        <Text variant="headlineMedium" style={s.title}>{t('billing_payment_title')}</Text>
        <ReadResourceState phase={resource.phase} retry={resource.retry} cancel={resource.cancel} />
        {data && (
          <Card style={{marginBottom: 8}}>
            <Card.Content>
              {data.method ? (
                <>
                  <Text style={s.meta}>{t('billing_payment_method')}: {t('billing_payment_card_brand')} {data.method.brand ?? ''} {t('billing_payment_card_last4')} {data.method.last4 ?? ''}</Text>
                  {data.method.exp_month && data.method.exp_year && (
                    <Text style={s.meta}>{t('billing_payment_card_expiry')}: {formatExpiry(data.method.exp_month, data.method.exp_year)}</Text>
                  )}
                </>
              ) : (
                <Text style={s.meta}>{t('billing_payment_no_method')}</Text>
              )}
            </Card.Content>
          </Card>
        )}
        <Button mode="outlined" onPress={handleManage}>{t('billing_payment_manage')}</Button>
      </View>
    </SafeAreaView>
  );
}
