// One typed function per endpoint of internal/api/server.go, in the same
// order as its route table. Pages and stores call these; nothing else in the
// app builds a URL.
import { request, url } from './client';
import type {
  AccountInput,
  AccountView,
  ChannelInput,
  ChannelType,
  ChannelView,
  Day,
  Definition,
  Delivery,
  ImportReport,
  Incident,
  IncidentState,
  IncidentView,
  Instant,
  List,
  MeterInput,
  MeterView,
  NewToken,
  Overview,
  Push,
  RecurRule,
  ReminderInput,
  ReminderView,
  Series,
  SeriesRange,
  SessionInfo,
  Settings,
  Spend,
  SubjectType,
  SystemInfo,
  TestInput,
  TestResult,
  Token,
  TokenInput,
} from './types';

const items = <T>(list: Promise<List<T>>): Promise<T[]> => list.then((body) => body.items);
const id = encodeURIComponent;

// ---------- session ----------

export const getSession = () => request<SessionInfo>('GET', '/session');
/** Rejects with code wrong_password (401), too_many_attempts (429, with
 *  retryAfter) or no_password (503). */
export const signIn = (password: string) =>
  request<{ signed_in: boolean }>('POST', '/session', { password }, { expect401: true });
export const signOut = () => request<{ signed_in: boolean }>('DELETE', '/session');

// ---------- overview ----------

export const getOverview = () => request<Overview>('GET', '/overview');

// ---------- accounts ----------

export const listAccounts = () => items(request<List<AccountView>>('GET', '/accounts'));
export const getAccount = (accountId: string) => request<AccountView>('GET', `/accounts/${id(accountId)}`);
export const createAccount = (input: AccountInput) => request<AccountView>('POST', '/accounts', input);
export const updateAccount = (accountId: string, input: AccountInput) =>
  request<AccountView>('PATCH', `/accounts/${id(accountId)}`, input);
export const deleteAccount = (accountId: string) => request<void>('DELETE', `/accounts/${id(accountId)}`);
/** Tries a credential without saving anything. A refusal by the platform is
 *  an answer ({ ok: false, error }), not a rejection. */
export const testAccount = (input: TestInput) => request<TestResult>('POST', '/accounts/test', input);
/** Asks for a fresh sample of one account, or of all of them. Answers at once
 *  (202); the result arrives as a `meters` change on the event stream. */
export const requestSample = (accountId?: string) =>
  request<{ requested: boolean }>('POST', accountId ? `/accounts/${id(accountId)}/sample` : '/sample');

// ---------- meters ----------

export const listMeters = () => items(request<List<MeterView>>('GET', '/meters'));
export const getMeter = (meterId: string) => request<MeterView>('GET', `/meters/${id(meterId)}`);
/** `rules`, when sent, replaces the meter's rules as a whole. */
export const updateMeter = (meterId: string, input: MeterInput) =>
  request<MeterView>('PATCH', `/meters/${id(meterId)}`, input);
export const deleteMeter = (meterId: string) => request<void>('DELETE', `/meters/${id(meterId)}`);
export const getSeries = (meterId: string, range: SeriesRange, signal?: AbortSignal) =>
  request<Series>('GET', `/meters/${id(meterId)}/series`, undefined, { query: { range }, ...(signal ? { signal } : {}) });
export const muteMeter = (meterId: string, until: Instant) =>
  request<MeterView>('PUT', `/meters/${id(meterId)}/mute`, { until });
export const unmuteMeter = (meterId: string) => request<MeterView>('DELETE', `/meters/${id(meterId)}/mute`);
/** Pushes readings from outside. The dashboard has little use for it; it is
 *  here so the list is complete. */
export const ingest = (readings: Push[]) =>
  request<{ accepted: number; meters: string[] }>('POST', '/ingest', { readings });

// ---------- reminders (the dashboard calls them dates) ----------

export const listReminders = () => items(request<List<ReminderView>>('GET', '/reminders'));
export const getReminder = (reminderId: string) => request<ReminderView>('GET', `/reminders/${id(reminderId)}`);
export const createReminder = (input: ReminderInput) => request<ReminderView>('POST', '/reminders', input);
export const updateReminder = (reminderId: string, input: ReminderInput) =>
  request<ReminderView>('PATCH', `/reminders/${id(reminderId)}`, input);
export const deleteReminder = (reminderId: string) => request<void>('DELETE', `/reminders/${id(reminderId)}`);
/** Shows where a rule lands without saving anything. */
export const previewReminder = (rule: RecurRule, signal?: AbortSignal) =>
  request<ReminderView>('POST', '/reminders/preview', { rule }, signal ? { signal } : {});
/** Marks the occurrence a reminder is currently about as dealt with. */
export const completeReminder = (reminderId: string) =>
  request<ReminderView>('POST', `/reminders/${id(reminderId)}/done`);
/** Takes back the last "done". */
export const reopenReminder = (reminderId: string) =>
  request<ReminderView>('DELETE', `/reminders/${id(reminderId)}/done`);
export const snoozeReminder = (reminderId: string, until: Day) =>
  request<ReminderView>('PUT', `/reminders/${id(reminderId)}/snooze`, { until });
export const unsnoozeReminder = (reminderId: string) =>
  request<ReminderView>('DELETE', `/reminders/${id(reminderId)}/snooze`);

// ---------- incidents, deliveries, spend ----------

export interface IncidentQuery {
  state?: IncidentState;
  subject_type?: SubjectType;
  subject_id?: string;
  limit?: number;
  /** Page backwards: incidents opened before this instant. */
  before?: Instant;
}
export const listIncidents = (query: IncidentQuery = {}) =>
  items(request<List<IncidentView>>('GET', '/incidents', undefined, { query: { ...query } }));
/** Answers with the bare incident: no headline or detail. */
export const ackIncident = (incidentId: string) => request<Incident>('POST', `/incidents/${id(incidentId)}/ack`);
export const unackIncident = (incidentId: string) => request<Incident>('DELETE', `/incidents/${id(incidentId)}/ack`);

export interface DeliveryQuery {
  incident?: string;
  channel?: string;
  limit?: number;
  /** Page backwards: deliveries with an id below this one. */
  before?: number;
}
export const listDeliveries = (query: DeliveryQuery = {}) =>
  items(request<List<Delivery>>('GET', '/deliveries', undefined, { query: { ...query } }));
export const retryDelivery = (deliveryId: number) => request<Delivery>('POST', `/deliveries/${deliveryId}/retry`);

/** month is "YYYY-MM"; the current month when left out. */
export const getSpend = (month?: string, signal?: AbortSignal) =>
  request<Spend>('GET', '/spend', undefined, { query: { month }, ...(signal ? { signal } : {}) });

// ---------- providers ----------

export const listProviders = () => items(request<List<Definition>>('GET', '/providers'));
export const saveProvider = (definition: Definition) =>
  request<Definition>('PUT', `/providers/${id(definition.id)}`, definition);
export const deleteProvider = (providerId: string) => request<void>('DELETE', `/providers/${id(providerId)}`);

// ---------- channels ----------

export const listChannelTypes = () => items(request<List<ChannelType>>('GET', '/channel-types'));
export const listChannels = () => items(request<List<ChannelView>>('GET', '/channels'));
export const createChannel = (input: ChannelInput) => request<ChannelView>('POST', '/channels', input);
export const updateChannel = (channelId: string, input: ChannelInput) =>
  request<ChannelView>('PATCH', `/channels/${id(channelId)}`, input);
export const deleteChannel = (channelId: string) => request<void>('DELETE', `/channels/${id(channelId)}`);
/** Sends a test message. A refusal by the platform is an answer
 *  ({ ok: false, error }), not a rejection. */
export const testChannel = (channelId: string) =>
  request<{ ok: boolean; error?: string }>('POST', `/channels/${id(channelId)}/test`);

// ---------- tokens ----------

export const listTokens = () => items(request<List<Token>>('GET', '/tokens'));
/** The answer carries the plaintext token, this once. */
export const createToken = (input: TokenInput) => request<NewToken>('POST', '/tokens', input);
export const deleteToken = (tokenId: string) => request<void>('DELETE', `/tokens/${id(tokenId)}`);

// ---------- settings and system ----------

export const getSettings = () => request<Settings>('GET', '/settings');
/** A partial update: send only what changed. `fx`, when sent, replaces the
 *  whole table of rates. */
export const updateSettings = (patch: Partial<Settings>) => request<Settings>('PATCH', '/settings', patch);
export const sendDigest = () => request<{ queued: boolean }>('POST', '/digest');
export const getSystem = () => request<SystemInfo>('GET', '/system');
export const backup = () => request<{ path: string }>('POST', '/system/backup');
/** The address of the export download; give it to a link, not to fetch. */
export const exportURL = (options: { secrets?: boolean; history?: boolean } = {}) =>
  url('/export', { secrets: options.secrets ? 1 : undefined, history: options.history === false ? 0 : undefined });
/** bundle is an export file's parsed JSON. */
export const importBundle = (bundle: unknown, options: { settings?: boolean } = {}) =>
  request<ImportReport>('POST', '/import', bundle, { query: { settings: options.settings ? 1 : undefined } });

// ---------- browser push ----------

/** What a browser subscribes with: the server's public key, base64url. */
export const getPushKey = () => request<{ public_key: string }>('GET', '/push');
/** Hands over a browser's push subscription (its toJSON()), which becomes a
 *  channel of type webpush. The same browser subscribing again refreshes its
 *  channel instead of adding one. */
export const subscribePush = (name: string, subscription: PushSubscriptionJSON) =>
  request<ChannelView>('POST', '/push/subscriptions', { name, subscription });
