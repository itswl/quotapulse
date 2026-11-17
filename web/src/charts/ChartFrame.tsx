import { ChartLine, Table } from 'lucide-preact';
import type { ComponentChildren } from 'preact';
import { useId, useState } from 'preact/hooks';
import { IconButton } from '../components/Button';
import { cx } from '../components/cx';
import { t } from '../i18n';

/** How a legend entry is keyed: mirroring the mark it explains. */
export type LegendKey = 'line' | 'projection' | 'threshold' | 'marker' | 'bar' | 'bar-partial';

export interface LegendEntry {
  key: LegendKey;
  label: ComponentChildren;
}

export interface ChartTable {
  columns: string[];
  /** Cells as they should read; numbers already formatted. */
  rows: string[][];
}

export interface ChartFrameProps {
  title: string;
  /** Explains the marks: what is dashed, what is lighter. Text stays in text
   *  colours; the little key beside it carries the series colour. */
  legend?: readonly LegendEntry[];
  /** The same data as rows, built only when someone asks for it. Every chart
   *  has one: it is how the numbers are read without hovering, or at all
   *  by someone who cannot see the plot. */
  table: () => ChartTable;
  /** Dims the plot while newer data is on its way, instead of blanking it. */
  refreshing?: boolean;
  class?: string;
  children: ComponentChildren;
}

/** A chart with its title, its legend, and a switch to the table view. */
export function ChartFrame({ title, legend, table, refreshing, class: extra, children }: ChartFrameProps) {
  const [asTable, setAsTable] = useState(false);
  const titleId = useId();
  return (
    <figure class={cx('chart-frame', extra)} aria-labelledby={titleId}>
      <figcaption class="chart-frame__head">
        <h3 class="chart-frame__title" id={titleId}>
          {title}
        </h3>
        {legend && legend.length > 0 && !asTable && (
          <ul class="chart-legend">
            {legend.map((entry) => (
              <li key={entry.key} class="chart-legend__item">
                <span class={cx('chart-legend__key', `chart-legend__key--${entry.key}`)} aria-hidden="true" />
                {entry.label}
              </li>
            ))}
          </ul>
        )}
        <IconButton
          class="chart-frame__toggle"
          size="sm"
          icon={asTable ? ChartLine : Table}
          label={asTable ? t('common.action.showChart') : t('common.action.showTable')}
          onClick={() => setAsTable(!asTable)}
        />
      </figcaption>
      <div class={cx('chart-frame__body', refreshing && 'chart-frame__body--refreshing')}>
        {asTable ? <DataTable data={table()} label={title} /> : children}
      </div>
    </figure>
  );
}

function DataTable({ data, label }: { data: ChartTable; label: string }) {
  return (
    // Focusable so the keyboard can scroll it.
    <div class="chart-table" tabIndex={0} role="group" aria-label={label}>
      <table>
        <thead>
          <tr>
            {data.columns.map((column, index) => (
              <th key={column} scope="col" class={index > 0 ? 'chart-table__num' : undefined}>
                {column}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {data.rows.map((row, index) => (
            <tr key={index}>
              {row.map((cell, column) =>
                column === 0 ? (
                  <th key={column} scope="row">
                    {cell}
                  </th>
                ) : (
                  <td key={column} class="chart-table__num num">
                    {cell}
                  </td>
                ),
              )}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
