/** Browser push: registers the service worker and manages the push subscription. */

import { request } from '../api/client.js';

export interface PushState {
  supported: boolean;
  enabled: boolean;
  permission: 'granted' | 'denied' | 'default' | 'unsupported';
}

function urlBase64ToUint8Array(base64: string): Uint8Array<ArrayBuffer> {
  const padding = '='.repeat((4 - (base64.length % 4)) % 4);
  const normalized = (base64 + padding).replace(/-/g, '+').replace(/_/g, '/');
  const binary = atob(normalized);
  const bytes = new Uint8Array(new ArrayBuffer(binary.length));
  for (let i = 0; i < binary.length; i += 1) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}

export async function pushState(): Promise<PushState> {
  if (typeof Notification === 'undefined' || !('serviceWorker' in navigator) || !('PushManager' in window)) {
    return { supported: false, enabled: false, permission: 'unsupported' };
  }
  const registration = await navigator.serviceWorker.getRegistration('/sw.js');
  const subscription = registration ? await registration.pushManager.getSubscription() : null;
  return { supported: true, enabled: Boolean(subscription), permission: Notification.permission };
}

/** Ask for permission, register the service worker, and subscribe with the server's VAPID key. */
export async function enablePush(): Promise<PushState> {
  if (typeof Notification === 'undefined' || !('serviceWorker' in navigator) || !('PushManager' in window)) {
    throw new Error('This browser does not support push notifications');
  }
  // iOS WebKit 只在点击手势的同步调用栈里弹权限窗——任何 await 之后请求都会被静默拒绝，
  // 所以 requestPermission 必须是第一件事。
  const permission = await Notification.requestPermission();
  if (permission !== 'granted') {
    throw new Error(permission === 'denied' ? 'Notifications are blocked for this site in the browser settings' : 'Notification permission was not granted');
  }
  const { publicKey } = await request<{ status: 'success'; publicKey: string }>('/api/push/config');
  const registration = await navigator.serviceWorker.register('/sw.js');
  const existing = await registration.pushManager.getSubscription();
  const subscription = existing ?? (await registration.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: urlBase64ToUint8Array(publicKey),
  }));
  const json = subscription.toJSON() as { endpoint?: string; keys?: { p256dh?: string; auth?: string } };
  await request('/api/push/subscribe', {
    method: 'POST',
    body: JSON.stringify({ endpoint: json.endpoint, keys: json.keys }),
  });
  return pushState();
}

/** Unsubscribe the browser and tell the server to forget the registration. */
export async function disablePush(): Promise<PushState> {
  const registration = await navigator.serviceWorker.getRegistration('/sw.js');
  const subscription = registration ? await registration.pushManager.getSubscription() : null;
  if (subscription) {
    const json = subscription.toJSON() as { endpoint?: string };
    try {
      await request('/api/push/unsubscribe', {
        method: 'POST',
        body: JSON.stringify({ endpoint: json.endpoint }),
      });
    } catch {
      // Server-side cleanup is best effort; the browser unsubscribe always applies.
    }
    await subscription.unsubscribe();
  }
  return pushState();
}

export interface TestPushResult {
  sent: number;
  failed: number;
  failures: string[];
}

/** Ask the server to deliver a test notification to every registered browser. */
export async function broadcastTestPush(): Promise<TestPushResult> {
  const result = await request<{ status: 'success'; sent: number; failed: number; failures?: string[] }>(
    '/api/push/test',
    { method: 'POST', body: JSON.stringify({}) },
  );
  return { sent: result.sent, failed: result.failed, failures: result.failures ?? [] };
}
