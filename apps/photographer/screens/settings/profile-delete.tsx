import {useLocale} from '../../src/locale-context';
import {Button, Card, HelperText, Text, TextInput} from 'react-native-paper';
import {useEffect, useMemo, useRef, useState} from 'react';
import {useLocalSearchParams, useRouter} from '../../src/localized-router';
import {SafeAreaView, View} from 'react-native';
import {useAuth} from '../../src/auth-context';
import {OwnerAPI} from '../../src/owner-api';
import {catalogStyle as s} from '../../src/catalog-style';

// UI-12 / UI-14: Profile delete screen.
// Criterion C (UI-14): deletion status uses the request_id scope, not the regular
// session token. After profileDelete() returns the request_id, statusPoll() calls
// GET /api/v1/me/deletion/{request_id}/status — a scoped capability endpoint that
// works even if the normal access token has been revoked (R7: status endpoint
// design is backend-owned; we only pass the request_id as scope here).
// User must type "DELETE" exactly (locale-invariant sentinel) — never invent alternate (R7).
const CONFIRM_SENTINEL = 'DELETE';
const POLL_INTERVAL_MS = 3000;
const POLL_MAX_TRIES = 10;

type DeletionStatus = {status: 'pending' | 'complete' | 'failed'};

async function scopedDeletionStatus(client: ReturnType<ReturnType<typeof useAuth>['client']>, requestId: string, signal: AbortSignal): Promise<DeletionStatus> {
  // Uses only the requestId as scope — not a full tenant/user session.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  return (client as any).request(`/api/v1/me/deletion/${encodeURIComponent(requestId)}/status`, {method: 'GET', signal}) as Promise<DeletionStatus>;
}

export default function ProfileDelete() {
  const {t} = useLocale();
  const {tenant} = useLocalSearchParams<{tenant: string}>();
  const auth = useAuth();
  const api = useMemo(() => new OwnerAPI(auth.client(), tenant || ''), [auth, tenant]);
  const router = useRouter();

  const [confirmText, setConfirmText] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [pendingId, setPendingId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const pollRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const triesRef = useRef(0);

  const confirmed = confirmText === CONFIRM_SENTINEL;

  useEffect(() => {
    if (!pendingId) return;
    const client = auth.client();
    let cancelled = false;
    const controller = new AbortController();
    const poll = async () => {
      if (cancelled) return;
      triesRef.current += 1;
      try {
        const result = await scopedDeletionStatus(client, pendingId, controller.signal);
        if (cancelled) return;
        if (result.status === 'complete') {
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          (router.replace as any)('/');
          return;
        }
        if (result.status === 'failed') {
          setError('profile_delete_failed');
          setPendingId(null);
          setSubmitting(false);
          return;
        }
      } catch {
        if (cancelled) return;
      }
      if (triesRef.current >= POLL_MAX_TRIES) {
        setError('profile_delete_timeout');
        setPendingId(null);
        setSubmitting(false);
        return;
      }
      if (!cancelled) pollRef.current = setTimeout(poll, POLL_INTERVAL_MS);
    };
    pollRef.current = setTimeout(poll, POLL_INTERVAL_MS);
    return () => {
      cancelled = true;
      controller.abort();
      if (pollRef.current) clearTimeout(pollRef.current);
    };
  }, [pendingId, auth, router]);

  const handleDelete = async () => {
    if (!confirmed) return;
    setSubmitting(true);
    setError(null);
    triesRef.current = 0;
    try {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const result = await (api as any).profileDelete() as {request_id: string};
      setPendingId(result.request_id);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'error');
      setSubmitting(false);
    }
  };

  const handleCancel = () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (router.back as any)();
  };

  return (
    <SafeAreaView style={s.root}>
      <View style={s.header}>
        <Text variant="headlineMedium" style={s.title}>{t('profile_delete_title')}</Text>
        <HelperText type="error" visible>
          {t('profile_delete_warning')}
        </HelperText>
        <Card style={{marginBottom: 8}}>
          <Card.Title title={t('profile_delete_consequences')} />
          <Card.Content>
            <Text style={s.meta}>{t('profile_delete_photos')}</Text>
            <Text style={s.meta}>{t('profile_delete_account')}</Text>
          </Card.Content>
        </Card>
        {!pendingId && (
          <>
            <TextInput
              label={t('profile_delete_confirm_label')}
              placeholder={t('profile_delete_confirm_placeholder')}
              value={confirmText}
              onChangeText={setConfirmText}
              autoCapitalize="characters"
              style={{marginBottom: 8}}
            />
            {error && <HelperText type="error" visible>{error}</HelperText>}
            <View style={{flexDirection: 'row', gap: 8}}>
              <Button mode="outlined" onPress={handleCancel} disabled={submitting}>{t('profile_delete_cancel')}</Button>
              <Button
                mode="contained"
                onPress={() => void handleDelete()}
                disabled={!confirmed || submitting}
                loading={submitting}
              >
                {t('profile_delete_proceed')}
              </Button>
            </View>
          </>
        )}
        {pendingId && (
          <Text accessibilityRole="alert" style={s.meta}>{t('profile_delete_pending')}</Text>
        )}
      </View>
    </SafeAreaView>
  );
}
