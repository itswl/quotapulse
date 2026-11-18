// The router's matching, and the translator's rules.
import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { lang, translate } from '../src/i18n';
import activity from '../src/i18n/areas/activity';
import common from '../src/i18n/areas/common';
import dates from '../src/i18n/areas/dates';
import meters from '../src/i18n/areas/meters';
import overview from '../src/i18n/areas/overview';
import settings from '../src/i18n/areas/settings';
import spend from '../src/i18n/areas/spend';
import { drawnByBrowser, isActive, matchPath } from '../src/router';

describe('matchPath', () => {
  it('matches fixed paths exactly', () => {
    assert.deepEqual(matchPath('/', '/'), {});
    assert.deepEqual(matchPath('/meters', '/meters'), {});
    assert.deepEqual(matchPath('/meters', '/meters/'), {});
    assert.equal(matchPath('/meters', '/meters/abc'), null);
    assert.equal(matchPath('/meters', '/dates'), null);
  });

  it('captures named segments', () => {
    assert.deepEqual(matchPath('/meters/:id', '/meters/mtr_123'), { id: 'mtr_123' });
    assert.equal(matchPath('/meters/:id', '/meters'), null);
    assert.equal(matchPath('/meters/:id', '/meters/a/b'), null);
    assert.deepEqual(matchPath('/meters/:id', '/meters/a%20b'), { id: 'a b' });
  });

  it('captures whatever follows a trailing star', () => {
    assert.deepEqual(matchPath('/dates/*', '/dates/new'), { rest: 'new' });
    assert.deepEqual(matchPath('/settings/:section/*', '/settings/channels/ch_1/edit'), { section: 'channels', rest: 'ch_1/edit' });
    assert.deepEqual(matchPath('/dates/*', '/dates'), { rest: '' });
  });
});

describe('drawnByBrowser', () => {
  it('leaves a step through history to the browser when it has slid the page in itself', () => {
    // A swipe from the edge of a phone's screen.
    assert.equal(drawnByBrowser({ hasUAVisualTransition: true }), true);
  });

  it('hands over itself when the browser says it drew nothing', () => {
    // The way back in the top bar, or a back button.
    assert.equal(drawnByBrowser({ hasUAVisualTransition: false }), false);
  });

  it('takes a browser that cannot say to have drawn it', () => {
    assert.equal(drawnByBrowser({}), true);
  });
});

describe('isActive', () => {
  it('marks a page and the pages beneath it, but the root only for itself', () => {
    assert.equal(isActive('/meters', '/meters'), true);
    assert.equal(isActive('/meters', '/meters/mtr_1'), true);
    assert.equal(isActive('/meters', '/metersx'), false);
    assert.equal(isActive('/', '/'), true);
    assert.equal(isActive('/', '/meters'), false);
  });
});

describe('translate', () => {
  it('fills in parameters', () => {
    assert.equal(translate('en', 'common.shell.version', { v: '1.2' }), 'Version 1.2');
    assert.equal(translate('zh', 'common.shell.version', { v: '1.2' }), '版本 1.2');
  });

  it('picks the singular for exactly one', () => {
    assert.equal(translate('en', 'overview.verdict.attention', { n: 1 }), '1 thing needs attention');
    assert.equal(translate('en', 'overview.verdict.attention', { n: 4 }), '4 things need attention');
    assert.equal(translate('zh', 'overview.verdict.attention', { n: 1 }), '1 件事需要处理');
  });

  it('returns the key itself for a key that does not exist', () => {
    assert.equal(translate('en', 'common.no.such.key'), 'common.no.such.key');
  });

  it('starts in a language the dashboard has', () => {
    assert.ok(lang.value === 'en' || lang.value === 'zh');
  });
});

describe('the words', () => {
  const areas = { common, overview, meters, dates, spend, activity, settings };

  it('are complete in both languages, with nothing left empty', () => {
    for (const [name, words] of Object.entries(areas)) {
      assert.deepEqual(Object.keys(words.zh).sort(), Object.keys(words.en).sort(), `${name}: the two languages differ`);
      for (const language of ['en', 'zh'] as const) {
        for (const [key, text] of Object.entries<string>(words[language])) {
          assert.ok(text.trim() !== '', `${name}.${key} is empty in ${language}`);
        }
      }
    }
  });

  it('use the same parameters in both languages', () => {
    const params = (text: string) => [...text.matchAll(/\{(\w+)\}/g)].map((match) => match[1]).sort();
    for (const [name, words] of Object.entries(areas)) {
      const zh: Record<string, string> = words.zh;
      for (const [key, text] of Object.entries<string>(words.en)) {
        const english = new Set(params(text));
        const chinese = new Set(params(zh[key] ?? ''));
        // {nth} is the English ordinal ("30th"); Chinese says it with {n}.
        english.delete('nth');
        if (english.has('n') || chinese.has('n')) {
          english.delete('n');
          chinese.delete('n');
        }
        assert.deepEqual([...chinese].sort(), [...english].sort(), `${name}.${key}`);
      }
    }
  });
});
