// Watching: everything QuotaPulse keeps an eye on, in one place — the meters
// and the dates — to look through, search, open and put right. Now says what
// is happening; this is where each thing is.
//
// A wide screen has the list and the open thing side by side. A narrow one
// has the list, and a meter opens in its place; a date opens as a sheet.
import { Search } from 'lucide-preact';
import { useState } from 'preact/hooks';
import { cx, Input } from '../../components';
import { useMedia, useTitle } from '../../hooks';
import { t } from '../../i18n';
import { Link, route } from '../../router';
import type { PageProps } from '../../routes';
import { meters } from '../../state/meters';
import { reminders } from '../../state/reminders';
import { useResource } from '../../state/resource';
import { DatesSegment } from '../dates/list';
import { MeterDetail } from '../meters/meter';
import { MetersSegment } from '../meters/list';

export default function WatchingPage({ params }: PageProps) {
  const segment = route.value.path.startsWith('/dates') ? 'dates' : 'meters';
  const meterId = segment === 'meters' ? params.id : undefined;
  const wide = useMedia('(min-width: 1024px)');
  const [search, setSearch] = useState('');
  useResource(meters);
  useResource(reminders);

  const open = meterId ? meters.data.value?.find((meter) => meter.id === meterId) : undefined;
  useTitle(open?.display ?? t('common.nav.watching'));

  if (meterId && !wide) {
    return (
      <div class="watching watching--single">
        <MeterDetail key={meterId} id={meterId} standalone />
      </div>
    );
  }

  const meterCount = meters.data.value?.filter((meter) => !meter.hidden).length;
  const dateCount = reminders.data.value?.length;
  const tabs = [
    { id: 'meters', href: '/meters', label: t('common.nav.meters'), count: meterCount },
    { id: 'dates', href: '/dates', label: t('common.nav.dates'), count: dateCount },
  ];

  return (
    <div class={cx('watching', wide && 'watching--wide')}>
      <header class="watching__head">
        <h1 class="watching__title">{t('common.nav.watching')}</h1>
        <nav class="seg-links" aria-label={t('common.nav.watching')}>
          {tabs.map((tab) => (
            <Link key={tab.id} href={tab.href} replace class={cx('seg-links__item', segment === tab.id && 'is-active')} aria-current={segment === tab.id ? 'page' : undefined}>
              {tab.label}
              {tab.count !== undefined && <span class="seg-links__count num">{tab.count}</span>}
            </Link>
          ))}
        </nav>
        <Input class="watching__search" type="search" value={search} onInput={setSearch} placeholder={t(segment === 'dates' ? 'dates.search' : 'meters.list.search')} aria-label={t(segment === 'dates' ? 'dates.search' : 'meters.list.search')} prefix={<Search size={15} />} />
      </header>
      {segment === 'meters' ? <MetersSegment search={search} selected={meterId} wide={wide} /> : <DatesSegment search={search} wide={wide} />}
    </div>
  );
}
