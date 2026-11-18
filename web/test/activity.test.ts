// The logic of the Activity page: paging backwards without losing anything,
// days, and what became of a notification.
import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import type { DeliveryQuery, IncidentQuery } from '../src/api/endpoints';
import type { Delivery, IncidentView } from '../src/api/types';
import { boldRuns, deliveryPager, deliveryState, dropReason, eventFeed, groupByDay, incidentPager, isLocalPath, loadWindow } from '../src/pages/activity/logic';

const SHANGHAI = 'Asia/Shanghai';

/** An incident with only what paging looks at. */
const incident = (id: string, opened_at: string) => ({ id, opened_at }) as IncidentView;

/** The server's side of the bargain (internal/store/incidents.go): newest
 *  first, strictly before `before`, at most `limit` — and 100 when asked for
 *  more than 500. */
function incidentServer(all: IncidentView[]) {
  const calls: IncidentQuery[] = [];
  const list = async (query: IncidentQuery): Promise<IncidentView[]> => {
    calls.push(query);
    const before = query.before ? Math.floor(Date.parse(query.before) / 1000) : Infinity;
    const limit = !query.limit || query.limit > 500 ? 100 : query.limit;
    return all.filter((each) => Date.parse(each.opened_at) / 1000 < before).slice(0, limit);
  };
  return { list, calls };
}

function deliveryServer(count: number) {
  const all = Array.from({ length: count }, (_, index) => ({ id: count - index }) as Delivery);
  return async (query: DeliveryQuery): Promise<Delivery[]> => all.filter((each) => each.id < (query.before ?? Infinity)).slice(0, query.limit ?? 100);
}

describe('paging through incidents', () => {
  it('takes the first page and knows there is more', async () => {
    const all = ['e', 'd', 'c', 'b', 'a'].map((id, index) => incident(id, `2026-11-16T10:00:0${5 - index}Z`));
    const page = await incidentPager(incidentServer(all).list)([], 2);
    assert.deepEqual(
      page.items.map((each) => each.id),
      ['e', 'd'],
    );
    assert.equal(page.more, true);
  });

  it('does not skip incidents that opened in the same second as the last one shown', async () => {
    // Five opened in the same second; the page ends in the middle of them.
    const all = [
      incident('g', '2026-11-16T10:00:02Z'),
      incident('f', '2026-11-16T10:00:01Z'),
      incident('e', '2026-11-16T10:00:01Z'),
      incident('d', '2026-11-16T10:00:01Z'),
      incident('c', '2026-11-16T10:00:01Z'),
      incident('b', '2026-11-16T10:00:01Z'),
      incident('a', '2026-11-16T10:00:00Z'),
    ];
    const pager = incidentPager(incidentServer(all).list, 'resolved');
    const first = await pager([], 3);
    assert.deepEqual(
      first.items.map((each) => each.id),
      ['g', 'f', 'e'],
    );
    const second = await pager(first.items, 3);
    assert.deepEqual(
      second.items.map((each) => each.id),
      ['d', 'c', 'b'],
    );
    assert.equal(second.more, true);
    const third = await pager([...first.items, ...second.items], 3);
    assert.deepEqual(
      third.items.map((each) => each.id),
      ['a'],
    );
    assert.equal(third.more, false);
  });

  it('passes the state on, and asks from one second after the last one shown', async () => {
    const server = incidentServer([incident('b', '2026-11-16T10:00:01Z'), incident('a', '2026-11-16T10:00:00Z')]);
    const pager = incidentPager(server.list, 'resolved');
    const first = await pager([], 1);
    await pager(first.items, 1);
    assert.deepEqual(server.calls, [
      { state: 'resolved', limit: 2 },
      { state: 'resolved', limit: 3, before: '2026-11-16T10:00:02.000Z' },
    ]);
  });

  it('ends when the server has nothing new', async () => {
    const all = [incident('a', '2026-11-16T10:00:00Z')];
    const page = await incidentPager(incidentServer(all).list)(all, 50);
    assert.deepEqual(page, { items: [], more: false });
  });
});

describe('paging through notifications', () => {
  it('pages by id until the end', async () => {
    const pager = deliveryPager(deliveryServer(5));
    const first = await pager([], 2);
    assert.deepEqual(
      first.items.map((each) => each.id),
      [5, 4],
    );
    assert.equal(first.more, true);
    const second = await pager(first.items, 2);
    const third = await pager([...first.items, ...second.items], 2);
    assert.deepEqual(
      third.items.map((each) => each.id),
      [1],
    );
    assert.equal(third.more, false);
  });

  it('says there is no more when the last page is exactly full', async () => {
    const page = await deliveryPager(deliveryServer(2))([], 2);
    assert.equal(page.items.length, 2);
    assert.equal(page.more, false);
  });
});

describe('loading several pages at once', () => {
  it('loads as many as asked for, a chunk at a time', async () => {
    const page = await loadWindow(deliveryPager(deliveryServer(10)), 7, 3);
    assert.deepEqual(
      page.items.map((each) => each.id),
      [10, 9, 8, 7, 6, 5, 4],
    );
    assert.equal(page.more, true);
  });

  it('stops at the end of the list', async () => {
    const page = await loadWindow(deliveryPager(deliveryServer(4)), 50, 3);
    assert.equal(page.items.length, 4);
    assert.equal(page.more, false);
  });

  it('copes with an empty list', async () => {
    assert.deepEqual(await loadWindow(deliveryPager(deliveryServer(0)), 50), { items: [], more: false });
  });
});

describe('the feed of events', () => {
  const open = [incident('open-new', '2026-11-18T09:00:00Z'), incident('open-old', '2026-10-01T09:00:00Z')];
  const resolved = [incident('done-a', '2026-11-17T09:00:00Z'), incident('done-b', '2026-11-10T09:00:00Z')];

  it('puts the open events among the resolved ones, newest first', () => {
    assert.deepEqual(
      eventFeed(open, resolved, false).map((each) => each.id),
      ['open-new', 'done-a', 'done-b', 'open-old'],
    );
  });

  it('holds back an open event older than everything loaded while there is more to load', () => {
    assert.deepEqual(
      eventFeed(open, resolved, true).map((each) => each.id),
      ['open-new', 'done-a', 'done-b'],
    );
  });

  it('shows every open event when nothing has been resolved yet', () => {
    assert.deepEqual(
      eventFeed(open, [], false).map((each) => each.id),
      ['open-new', 'open-old'],
    );
  });
});

describe('days', () => {
  it('groups by the day in the zone, not in UTC', () => {
    // 15:30 and 16:30 UTC are either side of midnight in Shanghai.
    const items = ['2026-11-16T16:30:00Z', '2026-11-16T15:30:00Z', '2026-11-16T02:00:00Z', '2026-11-14T02:00:00Z'];
    const groups = groupByDay(items, (at) => at, SHANGHAI);
    assert.deepEqual(groups, [
      { day: '2026-11-17', items: ['2026-11-16T16:30:00Z'] },
      { day: '2026-11-16', items: ['2026-11-16T15:30:00Z', '2026-11-16T02:00:00Z'] },
      { day: '2026-11-14', items: ['2026-11-14T02:00:00Z'] },
    ]);
  });

  it('makes nothing of nothing', () => {
    assert.deepEqual(
      groupByDay([], (at: string) => at, SHANGHAI),
      [],
    );
  });
});

describe('what became of a notification', () => {
  const now = Date.parse('2026-11-16T15:30:00Z');
  const pending = { status: 'pending' as const, attempts: 0, not_before: '2026-11-16T15:30:00Z' };

  it('takes a settled status as it is', () => {
    assert.equal(deliveryState({ ...pending, status: 'sent' }, now), 'sent');
    assert.equal(deliveryState({ ...pending, status: 'failed', attempts: 6 }, now), 'failed');
    assert.equal(deliveryState({ ...pending, status: 'dropped' }, now), 'dropped');
  });

  it('tells held back from about to go', () => {
    assert.equal(deliveryState(pending, now), 'queued');
    assert.equal(deliveryState({ ...pending, not_before: '2026-11-17T00:00:00Z' }, now), 'held');
    // The clock on screen can be half a minute behind the server's.
    assert.equal(deliveryState({ ...pending, not_before: '2026-11-16T15:30:30Z' }, now), 'queued');
  });

  it('calls a pending one that has already failed a retry', () => {
    assert.equal(deliveryState({ ...pending, attempts: 2, not_before: '2026-11-16T15:40:00Z' }, now), 'retrying');
  });

  it('recognises why the server did not send', () => {
    assert.equal(dropReason('muted: recorded, not sent'), 'muted');
    assert.equal(dropReason('resolved before it was sent'), 'resolved');
    assert.equal(dropReason('the channel was deleted'), 'channelDeleted');
    assert.equal(dropReason('410 Gone'), undefined);
    assert.equal(dropReason('constructor'), undefined);
  });
});

describe('a message', () => {
  it('splits a digest line at its bold markers', () => {
    assert.deepEqual(boldRuns('**DeepSeek** 547.67 CNY'), [
      { text: 'DeepSeek', bold: true },
      { text: ' 547.67 CNY', bold: false },
    ]);
    assert.deepEqual(boldRuns('Nov 19 · **iCloud+** · in 3 days'), [
      { text: 'Nov 19 · ', bold: false },
      { text: 'iCloud+', bold: true },
      { text: ' · in 3 days', bold: false },
    ]);
  });

  it('leaves a line without markers, or with a stray one, as it is', () => {
    assert.deepEqual(boldRuns('• Low balance'), [{ text: '• Low balance', bold: false }]);
    assert.deepEqual(boldRuns('2 ** 8 is 256'), [{ text: '2 ** 8 is 256', bold: false }]);
  });

  it('follows only paths inside the dashboard', () => {
    assert.equal(isLocalPath('/meters/mtr_1'), true);
    assert.equal(isLocalPath('//example.com'), false);
    assert.equal(isLocalPath('https://example.com'), false);
    assert.equal(isLocalPath(undefined), false);
  });
});
