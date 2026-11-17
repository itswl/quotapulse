// Browser push for the device at hand: what this browser can do, whether it
// is subscribed, and turning notifications on and off.
//
// The browser is the source of truth for "is this device on" — never
// localStorage. Its subscription's endpoint, hashed, is the `device` of the
// webpush channel that stands for it (logic.ts: deviceFingerprint), which
// also recognises a subscription carried over from an earlier install.
import { useCallback, useEffect, useState } from 'preact/hooks';
import { ApiError } from '../../api/client';
import { deleteChannel, getPushKey, subscribePush } from '../../api/endpoints';
import type { ChannelView } from '../../api/types';
import { deviceFingerprint, deviceName, keyBytes, sameBytes } from './logic';

export type PushSupport =
  | 'ok'
  /** Not https, nor localhost: browsers offer neither service workers nor push. */
  | 'insecure'
  /** An iPhone or iPad, in a browser tab: push only exists in an installed
   *  home-screen app. */
  | 'ios-install'
  /** An installed app on an iOS too old for push (before 16.4). */
  | 'ios-old'
  | 'unsupported';

function isIOS(): boolean {
  const ua = navigator.userAgent;
  return /iPad|iPhone|iPod/.test(ua) || (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1);
}

function isStandalone(): boolean {
  return (navigator as { standalone?: boolean }).standalone === true || (typeof matchMedia === 'function' && matchMedia('(display-mode: standalone)').matches);
}

export function pushSupport(): PushSupport {
  if (typeof Notification !== 'undefined' && 'serviceWorker' in navigator && 'PushManager' in window) return 'ok';
  if (!window.isSecureContext) return 'insecure';
  if (isIOS()) return isStandalone() ? 'ios-old' : 'ios-install';
  return 'unsupported';
}

export type PushPermission = NotificationPermission;

function permissionNow(): PushPermission {
  return typeof Notification === 'undefined' ? 'default' : Notification.permission;
}

/** The fingerprint of the subscription this browser holds; null without one. */
async function currentDevice(): Promise<{ subscription: PushSubscription; device: string } | null> {
  const registration = await navigator.serviceWorker.getRegistration('/');
  const subscription = await registration?.pushManager.getSubscription();
  if (!subscription) return null;
  return { subscription, device: await deviceFingerprint(subscription.endpoint) };
}

export interface PushState {
  support: PushSupport;
  permission: PushPermission;
  /** This browser's fingerprint while it holds a subscription; null when it
   *  holds none; undefined while the browser is being asked. */
  device: string | null | undefined;
  /** Asks the browser again. */
  refresh: () => void;
}

/** Follows what the browser says about push on this device. */
export function usePush(): PushState {
  const [support] = useState(pushSupport);
  const [permission, setPermission] = useState(permissionNow);
  const [device, setDevice] = useState<string | null | undefined>(support === 'ok' ? undefined : null);
  const [tick, setTick] = useState(0);

  useEffect(() => {
    if (support !== 'ok') return;
    let alive = true;
    setPermission(permissionNow());
    currentDevice()
      .then((current) => alive && setDevice(current?.device ?? null))
      .catch(() => alive && setDevice(null));
    return () => {
      alive = false;
    };
  }, [support, tick]);

  // Un-blocking happens in the browser's own settings, outside the page;
  // notice when it does.
  useEffect(() => {
    if (support !== 'ok' || !navigator.permissions) return;
    let status: PermissionStatus | undefined;
    const changed = () => setTick((n) => n + 1);
    navigator.permissions
      .query({ name: 'notifications' })
      .then((result) => {
        status = result;
        result.addEventListener('change', changed);
      })
      .catch(() => undefined);
    return () => status?.removeEventListener('change', changed);
  }, [support]);

  const refresh = useCallback(() => setTick((n) => n + 1), []);
  return { support, permission, device, refresh };
}

/** Why turning push on stopped short. */
export class PushError extends Error {
  override readonly name = 'PushError';
  constructor(
    /** denied: the browser blocks notifications for this site. dismissed:
     *  the prompt was closed without an answer. worker: the service worker
     *  did not start. subscribe: the browser's push service refused. */
    readonly reason: 'denied' | 'dismissed' | 'worker' | 'subscribe',
    message = '',
  ) {
    super(message);
  }
}

function within<T>(promise: Promise<T>, ms: number, otherwise: () => Error): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const timer = setTimeout(() => reject(otherwise()), ms);
    promise.then(
      (value) => {
        clearTimeout(timer);
        resolve(value);
      },
      (cause: unknown) => {
        clearTimeout(timer);
        reject(cause);
      },
    );
  });
}

/**
 * Turns notifications on for this device and returns its channel. Rejects
 * with a PushError, or with the ApiError of a request the server refused.
 */
export async function turnOnPush(): Promise<ChannelView> {
  // The permission prompt comes first, before anything else is awaited: iOS
  // only shows it inside the call stack of the tap that asked for it.
  const permission = await Notification.requestPermission();
  if (permission !== 'granted') throw new PushError(permission === 'denied' ? 'denied' : 'dismissed');

  const key = keyBytes((await getPushKey()).public_key);

  let registration: ServiceWorkerRegistration;
  try {
    registration = await navigator.serviceWorker.register('/sw.js');
    await within(navigator.serviceWorker.ready, 15_000, () => new Error('the service worker did not become active'));
  } catch (cause) {
    throw new PushError('worker', cause instanceof Error ? cause.message : String(cause));
  }

  let subscription: PushSubscription;
  try {
    let existing = await registration.pushManager.getSubscription();
    // A subscription made with another key — an earlier install's — cannot
    // be delivered to with ours. Start it over.
    if (existing && !sameBytes(existing.options.applicationServerKey, key)) {
      await existing.unsubscribe();
      existing = null;
    }
    subscription = existing ?? (await registration.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: key }));
  } catch (cause) {
    if (cause instanceof DOMException && cause.name === 'NotAllowedError') throw new PushError(permissionNow() === 'denied' ? 'denied' : 'dismissed');
    throw new PushError('subscribe', cause instanceof Error ? cause.message : String(cause));
  }

  return subscribePush(deviceName(navigator.userAgent, navigator.maxTouchPoints > 1), subscription.toJSON());
}

/** Turns notifications off for this device: the browser forgets its
 *  subscription, and the channel that stood for it is deleted. */
export async function turnOffPush(channel: ChannelView | undefined): Promise<void> {
  const current = await currentDevice().catch(() => null);
  await current?.subscription.unsubscribe();
  if (!channel) return;
  try {
    await deleteChannel(channel.id);
  } catch (cause) {
    // Already gone — the server drops a channel whose subscription expired.
    if (!(cause instanceof ApiError && cause.status === 404)) throw cause;
  }
}
