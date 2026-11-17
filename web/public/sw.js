/* QuotaPulse's service worker. It exists for one thing: showing a
   notification when a push arrives and no tab is open. It caches nothing
   and answers no requests, so the dashboard always comes from the server. */

self.addEventListener('install', () => {
  // Replace whichever version was here before without waiting for every
  // tab to close: there is no cached app for the two to disagree about.
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    (async () => {
      // Versions before 1.0 kept a copy of the page. Nothing reads caches any
      // more; clear out what was left behind.
      for (const name of await caches.keys()) await caches.delete(name);
      await self.clients.claim();
    })(),
  );
});

self.addEventListener('push', (event) => {
  let message = { title: 'QuotaPulse', body: '' };
  try {
    if (event.data) message = { ...message, ...event.data.json() };
  } catch {
    message.body = event.data ? event.data.text() : '';
  }
  event.waitUntil(
    self.registration.showNotification(message.title || 'QuotaPulse', {
      body: message.body || '',
      icon: '/icons/icon-192.png',
      badge: '/icons/badge-96.png',
      // One notification per incident: an escalation or a recovery replaces
      // what was said before instead of piling up next to it.
      tag: message.tag || 'quotapulse',
      renotify: true,
      data: { url: message.url || '/' },
    }),
  );
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const data = event.notification.data || {};
  const target = new URL(data.url || '/', self.location.origin).href;
  event.waitUntil(
    (async () => {
      const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
      for (const client of windows) {
        if (new URL(client.url).origin !== self.location.origin) continue;
        await client.focus();
        if (client.url !== target && 'navigate' in client) {
          try {
            await client.navigate(target);
          } catch {
            // A window this worker does not control cannot be steered; it is
            // in front now, which is most of what was asked for.
          }
        }
        return;
      }
      await self.clients.openWindow(target);
    })(),
  );
});
