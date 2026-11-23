// Who is at the keyboard: nobody yet, or the one person who knows the
// password. Everything else in state/ is only loaded once this says so.
import { computed, signal } from '@preact/signals';
import { ApiError, onUnauthorized } from '../api/client';
import * as api from '../api/endpoints';
import type { SessionInfo, SetupInput } from '../api/types';
import { setTimezone, syncClock } from './clock';
import { startLive, stopLive } from './live';
import { resetResources } from './resource';

export type SessionStatus =
  /** the first check has not answered yet */
  | 'checking'
  | 'signedOut'
  | 'signedIn'
  /** the server could not be reached at all */
  | 'unreachable';

export const sessionStatus = signal<SessionStatus>('checking');
/** What the server said about itself: version, demo, whether a password is set. */
export const sessionInfo = signal<SessionInfo | undefined>(undefined);
/** True while notifications are recorded but not sent. */
export const muted = signal(false);

export const signedIn = computed(() => sessionStatus.value === 'signedIn');

/** Asks the server who we are. Called once at boot, and again whenever the
 *  answer may have changed. */
export async function checkSession(): Promise<boolean> {
  try {
    const info = await api.getSession();
    sessionInfo.value = info;
    muted.value = info.muted;
    // Before any page draws a time: the server's clock and zone.
    if (info.now) syncClock(info.now);
    if (info.timezone) setTimezone(info.timezone);
    if (info.signed_in) enter();
    else leave();
    return info.signed_in;
  } catch {
    if (sessionStatus.peek() !== 'signedIn') sessionStatus.value = 'unreachable';
    // Unreachable is not signed out: keep what is on screen, and let the
    // stream's own retry find the server again.
    return sessionStatus.peek() === 'signedIn';
  }
}

/** Signs in. Rejects with the ApiError the sign-in form shows. */
export async function signIn(password: string): Promise<void> {
  await api.signIn(password);
  if (!(await checkSession())) throw new ApiError(0, 'unknown', 'signed in, but the session did not stick');
}

/** Chooses the password of an install that has none, and signs in with it.
 *  Rejects with the ApiError the setup form shows. */
export async function setUp(input: SetupInput): Promise<void> {
  await api.setUp(input);
  if (!(await checkSession())) throw new ApiError(0, 'unknown', 'set up, but the session did not stick');
}

export async function signOut(): Promise<void> {
  try {
    await api.signOut();
  } finally {
    leave();
  }
}

function enter(): void {
  if (sessionStatus.peek() === 'signedIn') return;
  sessionStatus.value = 'signedIn';
  startLive(checkSession);
}

function leave(): void {
  stopLive();
  resetResources();
  sessionStatus.value = 'signedOut';
}

// Any request answered with 401 means the cookie is gone or expired.
onUnauthorized(() => {
  if (sessionStatus.peek() === 'signedIn') leave();
});
