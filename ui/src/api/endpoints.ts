/* Implementation note. */

import { fetchJson, request } from './client.js';
import type {
  CreditsResponse,
  EmailAlertRecord,
  EmailScanState,
  EmailSuppression,
  EmailsConfigResponse,
  FeaturesResponse,
  HealthResponse,
  HistoryListResponse,
  JobsResponse,
  MailboxPayload,
  ProjectPayload,
  ProjectsConfigResponse,
  ProvidersResponse,
  RefreshResponse,
  SubscriptionPayload,
  SubscriptionsConfigResponse,
  SubscriptionsResponse,
  TrendResponse,
} from './types.js';
import type { FetchResult } from './client.js';

// Implementation note.

export const getFeatures = (): Promise<FeaturesResponse> => request<FeaturesResponse>('/api/features');

export const getCredits = (): Promise<CreditsResponse> => request<CreditsResponse>('/api/credits');

/* Implementation note. */
export const getSubscriptions = (noCache = false): Promise<SubscriptionsResponse> =>
  request<SubscriptionsResponse>(
    '/api/subscriptions',
    noCache ? { headers: { 'Cache-Control': 'no-cache', Pragma: 'no-cache' } } : {},
  );

export const getSubscriptionsConfig = (): Promise<SubscriptionsConfigResponse> =>
  request<SubscriptionsConfigResponse>('/api/config/subscriptions');

export const getProviders = (): Promise<ProvidersResponse> => request<ProvidersResponse>('/api/providers');

export const getProjectsConfig = (): Promise<ProjectsConfigResponse> =>
  request<ProjectsConfigResponse>('/api/config/projects');

export const getMailboxes = (): Promise<EmailsConfigResponse> => request<EmailsConfigResponse>('/api/config/emails');

export const getEmailScanState = (): Promise<EmailScanState> => request<EmailScanState>('/api/email/scan');

export const getEmailHistory = (days = 30, limit = 100): Promise<HistoryListResponse<EmailAlertRecord>> =>
  request<HistoryListResponse<EmailAlertRecord>>(`/api/history/email-alerts?days=${days}&limit=${limit}`);

export const getEmailSuppressions = (): Promise<HistoryListResponse<EmailSuppression>> =>
  request<HistoryListResponse<EmailSuppression>>('/api/email/suppressions');

export const getHealth = (): Promise<HealthResponse> => request<HealthResponse>('/health');

export const getJobs = (): Promise<JobsResponse> => request<JobsResponse>('/api/jobs');

/**
 * Implementation note.
 * Implementation note.
 */
export const getTrend = (provider: string, projectName: string, days = 30): Promise<FetchResult<TrendResponse>> =>
  fetchJson<TrendResponse>(`/api/history/trend/${encodeURIComponent(`${provider}:${projectName}`)}?days=${days}`);

// Implementation note.

export const refresh = (projectName?: string): Promise<RefreshResponse> =>
  request<RefreshResponse>('/api/refresh', {
    method: 'POST',
    body: JSON.stringify(projectName ? { project_name: projectName } : {}),
  });

export const runEmailScan = (days: number): Promise<EmailScanState> =>
  request<EmailScanState>('/api/email/scan', { method: 'POST', body: JSON.stringify({ days }) });

// Implementation note.
// Implementation note.
export const ENDPOINTS = {
  saveProject: '/api/config/project',
  deleteProject: '/api/config/project/delete',
  addSubscription: '/api/subscription/add',
  updateSubscription: '/api/config/subscription',
  deleteSubscription: '/api/subscription/delete',
  markRenewed: '/api/subscription/mark_renewed',
  clearRenewed: '/api/subscription/clear_renewed',
  testNotify: '/api/notify/test',
  snoozeSubscription: '/api/subscription/snooze',
  subscriptionTimezone: '/api/subscription/timezone',
  subscriptionWebhook: '/api/subscription/webhook',
  emailSuppressionAdd: '/api/email/suppression',
  emailSuppressionDelete: '/api/email/suppression/delete',
  pushConfig: '/api/push/config',
  pushSubscribe: '/api/push/subscribe',
  pushUnsubscribe: '/api/push/unsubscribe',
  pushTest: '/api/push/test',
    saveEmail: '/api/config/email',
  deleteEmail: '/api/config/email/delete',
} as const;

export type { MailboxPayload, ProjectPayload, SubscriptionPayload };
