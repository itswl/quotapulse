// The answers pages keep for coming back to them.
import assert from 'node:assert/strict';
import { afterEach, describe, it } from 'node:test';
import { lang } from '../src/i18n';
import { forget, keep, recall } from '../src/state/kept';

describe('kept answers', () => {
  afterEach(() => forget());

  it('gives back the last answer for a key, and nothing for one never asked', () => {
    keep('spend:2026-11', { total: 1 });
    keep('spend:2026-11', { total: 2 });
    assert.deepEqual(recall('spend:2026-11'), { total: 2 });
    assert.equal(recall('spend:2026-10'), undefined);
  });

  it('lets the ones longest unanswered go when there are too many', () => {
    keep('series:first', 1);
    for (let i = 0; i < 39; i++) keep(`series:${i}`, i);
    // Answered again: now the latest.
    keep('series:first', 1);
    keep('series:one-too-many', 40);
    assert.equal(recall('series:first'), 1);
    assert.equal(recall('series:0'), undefined);
    assert.equal(recall('series:1'), 1);
    assert.equal(recall('series:one-too-many'), 40);
  });

  it('forgets everything when the session ends', () => {
    keep('incidents:open', []);
    forget();
    assert.equal(recall('incidents:open'), undefined);
  });

  it('forgets everything when the language changes: the server words its answers in it', () => {
    const was = lang.value;
    keep('incidents:open', ['in one language']);
    lang.value = was === 'en' ? 'zh' : 'en';
    try {
      assert.equal(recall('incidents:open'), undefined);
    } finally {
      lang.value = was;
    }
  });
});
