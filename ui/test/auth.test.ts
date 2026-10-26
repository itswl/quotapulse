/** The key is only kept in memory after the server has minted the session. */

import './stub-dom.js';

import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { getApiKey, saveApiKey, setApiKey } from '../src/api/client.js';

interface Call {
  url: string;
  init: RequestInit;
}

function stubFetch(status: number, calls: Call[]): void {
  globalThis.fetch = (async (input: string | URL | Request, init: RequestInit = {}) => {
    calls.push({ url: String(input), init });
    return new Response('{}', { status });
  }) as typeof fetch;
}

describe('saveApiKey', () => {
  const originalFetch = globalThis.fetch;

  it('会话建立成功后才保留密钥，并把它换成 Cookie', async () => {
    const calls: Call[] = [];
    stubFetch(200, calls);
    setApiKey(null);
    try {
      assert.equal(await saveApiKey(' secret '), true);
      assert.equal(getApiKey(), 'secret');
      assert.equal(calls.length, 1);
      const call = calls[0]!;
      assert.equal(call.url, 'http://localhost:8080/api/session');
      assert.equal(call.init.method, 'POST');
      assert.equal((call.init.headers as Record<string, string>)['X-API-Key'], 'secret');
    } finally {
      globalThis.fetch = originalFetch;
      setApiKey(null);
    }
  });

  it('服务端拒绝的密钥不会覆盖当前登录', async () => {
    const calls: Call[] = [];
    stubFetch(401, calls);
    setApiKey('working');
    try {
      assert.equal(await saveApiKey('wrong'), false);
      assert.equal(getApiKey(), 'working');
    } finally {
      globalThis.fetch = originalFetch;
      setApiKey(null);
    }
  });

  it('清空时同时删除服务端会话', async () => {
    const calls: Call[] = [];
    stubFetch(200, calls);
    setApiKey('old');
    try {
      assert.equal(await saveApiKey(''), true);
      assert.equal(getApiKey(), '');
      const call = calls[0]!;
      assert.equal(call.init.method, 'DELETE');
      assert.equal((call.init.headers as Record<string, string>)['X-API-Key'], 'old');
    } finally {
      globalThis.fetch = originalFetch;
      setApiKey(null);
    }
  });
});
