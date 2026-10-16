/* QuotaPulse service worker: Web Push notifications, plus a runtime cache so
   the installed PWA opens with something sensible offline — the last rendered
   shell — instead of the browser's error page. API calls always go to the
   network; the app already degrades gracefully when they fail. */

const RUNTIME_CACHE = 'quotapulse-runtime-v1';

self.addEventListener('fetch', (event) => {
  const request = event.request;
  if (request.method !== 'GET') return;

  let url;
  try {
    url = new URL(request.url);
  } catch {
    return;
  }
  if (url.origin !== self.location.origin) return;
  if (url.pathname.startsWith('/api/') || url.pathname === '/mcp' || url.pathname === '/health' || url.pathname === '/live') return;

  if (request.mode === 'navigate') {
    event.respondWith(
      fetch(request)
        .then((response) => {
          // The response type for a fresh navigations is `basic` only when
          // served from the same origin; only those are cacheable.
          if (response.ok || response.type === 'basic') {
            const copy = response.clone();
            caches.open(RUNTIME_CACHE).then((cache) => cache.put('/', copy));
          }
          return response;
        })
        .catch(() =>
          caches.match('/').then(
            (shell) =>
              shell ||
              new Response('<h1>Offline</h1><p>QuotaPulse needs a connection the first time it opens.</p>', {
                headers: { 'Content-Type': 'text/html; charset=utf-8' },
              }),
          ),
        ),
    );
    return;
  }

  // Hashed static assets (?v=<sha256>) never change for their URL.
  if (url.pathname.startsWith('/static/')) {
    event.respondWith(
      caches.match(request).then(
        (hit) =>
          hit ||
          fetch(request).then((response) => {
            if (response.ok) {
              const copy = response.clone();
              caches.open(RUNTIME_CACHE).then((cache) => cache.put(request, copy));
            }
            return response;
          }),
      ),
    );
  }
});

self.addEventListener('push', (event) => {
  let data = { title: 'QuotaPulse', body: '' };
  try {
    if (event.data) {
      data = { ...data, ...event.data.json() };
    }
  } catch (error) {
    data.body = event.data ? event.data.text() : '';
  }
  event.waitUntil(
    self.registration.showNotification(data.title || 'QuotaPulse', {
      body: data.body || '',
      icon: '/static/icons/icon-192.png',
      badge: '/static/icons/icon-192.png',
      tag: 'quotapulse-alert',
      renotify: true,
      data: { url: data.url || '/' },
    }),
  );
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const url = (event.notification.data && event.notification.data.url) || '/';
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((clients) => {
      for (const client of clients) {
        if (new URL(client.url).pathname === '/' && 'focus' in client) {
          return client.focus();
        }
      }
      return self.clients.openWindow(url);
    }),
  );
});
