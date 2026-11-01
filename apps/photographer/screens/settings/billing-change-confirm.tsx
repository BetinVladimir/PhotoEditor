import {useLocale} from '../../src/locale-context';
import {Button, Card, Divider, HelperText, Text} from 'react-native-paper';
import {useMemo} from 'react';
import {useLocalSearchParams, useRouter} from '../../src/localized-router';
import {SafeAreaView, View} from 'react-native';
import {useAuth} from '../../src/auth-context';
import {OwnerAPI} from '../../src/owner-api';
import {useReadResource} from '../../src/use-read-resource';
import {ReadResourceState} from '../../src/read-resource-state';
import {catalogStyle as s} from '../../src/catalog-style';

// Local DTO — mirrors billing change-preview endpoint (DTO 09 §7).
// Criteria E: price and tenant never come from route params.
type ChangePreviewMoney = {currency: string; net_minor: string; gross_minor: string; tax_minor: string; tax_mode: string};
type ResourceDelta = {resource_code: string; current_quantity: string; new_quantity: string};
type ChangePreview = {
  current_plan_code: string;
  new_plan_code: string;
  new_plan_name_key: string;
  surcharge: ChangePreviewMoney | null;
  resource_deltas: ResourceDelta[];
  credits_kept: string | null;
  anchor_amount: ChangePreviewMoney | null;
  next_recurring_total: ChangePreviewMoney | null;
  effective_at: string | null;
  projected_overquota_resources: string[];
};

export default function BillingChangeConfirm() {
  const {t} = useLocale();
  // planCode is used only as an API parameter, never displayed as a price (Criteria E).
  const {tenant, planCode} = useLocalSearchParams<{tenant: string; planCode: string}>();
  const auth = useAuth();
  const api = useMemo(() => new OwnerAPI(auth.client(), tenant || ''), [auth, tenant]);

  const resource = useReadResource(
    JSON.stringify([auth.origin, tenant, 'billing-change-preview', planCode]),
    api,
    async signal => {
      if (!tenant || !planCode) throw new Error('Workspace and plan required');
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      return (api as any).billingChangePreview(planCode, signal) as Promise<ChangePreview>;
    },
  );

  const data = resource.data;
  const router = useRouter();
  const handleConfirm = async () => {
    if (!tenant || !planCode) return;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    await (api as any).billingChangePlan(planCode);
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (router.push as any)({pathname: '/settings/billing', params: {tenant}});
  };
  const handleCancel = () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (router.back as any)();
  };

  const formatMoney = (m: ChangePreviewMoney | null | undefined) => {
    if (!m) return '';
    return `${m.gross_minor} ${m.currency.toUpperCase()} (${m.net_minor} ${t('billing_tax_net')})`;
  };

  return (
    <SafeAreaView style={s.root}>
      <View style={s.header}>
        <Text variant="headlineMedium" style={s.title}>{t('billing_change_confirm_title')}</Text>
        <ReadResourceState phase={resource.phase} retry={resource.retry} cancel={resource.cancel} />
        {!data && resource.phase === 'ready' && (
          <Text style={s.empty}>{t('billing_change_confirm_empty')}</Text>
        )}
        {data && (
          <Card style={{marginBottom: 8}}>
            <Card.Content>
              <Text style={s.meta}>{t('billing_change_confirm_current_plan')}: {data.current_plan_code}</Text>
              <Text style={s.meta}>{t('billing_change_confirm_new_plan')}: {data.new_plan_name_key}</Text>
              <Divider style={{marginVertical: 8}} />
              {data.surcharge && (
                <Text style={s.meta}>{t('billing_change_confirm_surcharge')}: {formatMoney(data.surcharge)}</Text>
              )}
              {data.resource_deltas.map(d => (
                <Text key={d.resource_code} style={s.meta}>
                  {t('billing_change_confirm_resource_delta')}: {d.resource_code} {d.current_quantity} → {d.new_quantity}
                </Text>
              ))}
              {data.credits_kept && (
                <Text style={s.meta}>{t('billing_change_confirm_credits_kept')}: {data.credits_kept}</Text>
              )}
              {data.anchor_amount && (
                <Text style={s.meta}>{t('billing_change_confirm_anchor')}: {formatMoney(data.anchor_amount)}</Text>
              )}
              {data.next_recurring_total && (
                <Text style={s.meta}>{t('billing_change_confirm_next_total')}: {formatMoney(data.next_recurring_total)}</Text>
              )}
              {data.effective_at && (
                <Text style={s.meta}>{t('billing_change_confirm_effective_date')}: {data.effective_at}</Text>
              )}
              {data.projected_overquota_resources.length > 0 && (
                <HelperText type="error" visible>
                  {t('billing_change_confirm_projected_overquota')}: {data.projected_overquota_resources.join(', ')}
                </HelperText>
              )}
            </Card.Content>
          </Card>
        )}
        {data && (
          <View style={{flexDirection: 'row', gap: 8}}>
            <Button mode="outlined" onPress={handleCancel}>{t('billing_change_confirm_cancel')}</Button>
            <Button mode="contained" onPress={handleConfirm}>{t('billing_change_confirm_proceed')}</Button>
          </View>
        )}
      </View>
    </SafeAreaView>
  );
}
