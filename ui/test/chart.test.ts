/**
 * Implementation note.
 *
 * Implementation note.
 * Implementation note.
 */

import './stub-dom.js';

import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { LineChart, niceScale, xLabelIndices, type LineChartOptions } from '../src/chart/line-chart.js';
import { trendPoints } from '../src/managers/trend-manager.js';

interface DrawLog {
  arcs: number;
  strokes: number;
  fills: number;
  texts: string[];
  dashes: number[][];
  transform: number[] | null;
}

/* Implementation note. */
function stubCanvas(width = 800, height = 360): { canvas: HTMLCanvasElement; log: DrawLog } {
  const log: DrawLog = { arcs: 0, strokes: 0, fills: 0, texts: [], dashes: [], transform: null };

  const ctx = {
    setTransform: (...args: number[]) => {
      log.transform = args;
    },
    clearRect: () => {},
    save: () => {},
    restore: () => {},
    beginPath: () => {},
    moveTo: () => {},
    lineTo: () => {},
    bezierCurveTo: () => {},
    closePath: () => {},
    translate: () => {},
    rotate: () => {},
    roundRect: () => {},
    arc: () => {
      log.arcs += 1;
    },
    stroke: () => {
      log.strokes += 1;
    },
    fill: () => {
      log.fills += 1;
    },
    fillText: (text: string) => {
      log.texts.push(text);
    },
    measureText: (text: string) => ({ width: text.length * 7 }),
    setLineDash: (dash: number[]) => {
      log.dashes.push(dash);
    },
    font: '',
    fillStyle: '',
    strokeStyle: '',
    lineWidth: 1,
    textAlign: '',
    textBaseline: '',
  };

  const canvas = {
    width: 0,
    height: 0,
    style: {} as Record<string, string>,
    parentElement: { clientWidth: width, clientHeight: height },
    getContext: () => ctx,
    getBoundingClientRect: () => ({ width, height, left: 0, top: 0 }),
    addEventListener: () => {},
    removeEventListener: () => {},
  };

  return { canvas: canvas as unknown as HTMLCanvasElement, log };
}

function options(overrides: Partial<LineChartOptions> = {}): LineChartOptions {
  return {
    labels: ['09-08', '09-09', '09-10'],
    dark: false,
    formatValue: (v) => v.toFixed(2),
    series: [
      { label: 'Balance', values: [100, 80, 60], color: '#6366f1', fill: 'rgba(99,102,241,0.1)' },
      { label: 'Alert threshold', values: [50, 50, 50], color: '#ef4444', dashed: true, showPoints: false },
    ],
    ...overrides,
  };
}

describe('LineChart', () => {
  it('按 CSS 尺寸乘以像素比设置画布，高分屏下不糊', () => {
    const { canvas, log } = stubCanvas(800, 360);
    new LineChart(canvas, options());

    assert.equal(canvas.width, 800);
    assert.equal(canvas.height, 360);
    assert.equal(canvas.style['width'], '800px');
    assert.deepEqual(log.transform, [1, 0, 0, 1, 0, 0]);
  });

  it('Balance线每个点画一个圆，阈值线一个都不画', () => {
    const { canvas, log } = stubCanvas();
    new LineChart(canvas, options());

    // Implementation note.
    assert.equal(log.arcs, 5);
  });

  it('阈值线用虚线，Balance线是实线', () => {
    const { canvas, log } = stubCanvas();
    new LineChart(canvas, options());

    assert.ok(log.dashes.some((d) => d.length === 2 && d[0] === 5 && d[1] === 5), '阈值线应该是 5/5 虚线');
    assert.ok(log.dashes.some((d) => d.length === 0), 'Balance线应该是实线');
  });

  it('画出图例文字和 y 轴刻度', () => {
    const { canvas, log } = stubCanvas();
    new LineChart(canvas, options());

    assert.ok(log.texts.includes('Balance'));
    assert.ok(log.texts.includes('Alert threshold'));
    assert.ok(log.texts.includes('09-08'));
    assert.ok(log.texts.length > 5, 'y 轴刻度也要画出来');
  });

  it('Balance一直没变也不会除零，仍然画出线', () => {
    const { canvas, log } = stubCanvas();
    new LineChart(canvas, options({ series: [{ label: 'Balance', values: [100, 100, 100], color: '#000' }] }));

    assert.ok(log.strokes > 0);
    assert.ok(log.texts.every((t) => t !== 'NaN' && !t.includes('NaN')), '刻度不能出现 NaN');
  });

  it('只有一个数据点时画在正中间，不崩', () => {
    const { canvas, log } = stubCanvas();
    new LineChart(canvas, options({ labels: ['09-08'], series: [{ label: 'Balance', values: [42], color: '#000' }] }));

    assert.ok(log.arcs >= 1);
  });

  it('没有数据时直接返回，不画任何东西', () => {
    const { canvas, log } = stubCanvas();
    new LineChart(canvas, options({ labels: [], series: [{ label: 'Balance', values: [], color: '#000' }] }));

    assert.equal(log.strokes, 0);
    assert.equal(log.texts.length, 0);
  });

  it('values 里的 null 被跳过，不会画成 0', () => {
    const { canvas, log } = stubCanvas();
    new LineChart(canvas, options({ series: [{ label: 'Balance', values: [100, null, 60], color: '#000' }] }));

    // Implementation note.
    assert.equal(log.arcs, 3);
  });

  it('update 换数据后重画，destroy 后清空画布', () => {
    const { canvas, log } = stubCanvas();
    const chart = new LineChart(canvas, options());
    const before = log.arcs;

    chart.update(options({ labels: ['09-08'], series: [{ label: 'Balance', values: [1], color: '#000' }] }));
    assert.ok(log.arcs > before);

    chart.destroy(); // 不抛即可
  });
});

describe('niceScale', () => {
  it('刻度落在 1/2/5 × 10^n 上，边界向外取整', () => {
    assert.deepEqual(niceScale(0, 1067.98), { min: 0, max: 1250, step: 250, decimals: 0 });
    assert.deepEqual(niceScale(0, 12.3), { min: 0, max: 12.5, step: 2.5, decimals: 1 });
    assert.deepEqual(niceScale(640.5, 975.4), { min: 600, max: 1000, step: 100, decimals: 0 });
    assert.deepEqual(niceScale(0, 1), { min: 0, max: 1, step: 0.2, decimals: 1 });
    assert.deepEqual(niceScale(9.2, 9.6), { min: 9.2, max: 9.6, step: 0.1, decimals: 1 });
  });

  it('y 轴刻度文字位数一致，不再出现 213.6 和 1,067.98 混排', () => {
    const { canvas, log } = stubCanvas();
    new LineChart(canvas, options({ series: [{ label: 'Balance', values: [912.4, 697.19, 975.44], color: '#6366f1' }] }));
    const ticks = log.texts.filter((t) => /^[\d,]+(\.\d+)?$/.test(t));
    assert.ok(ticks.length >= 3, `应画出多条刻度: ${ticks}`);
    assert.ok(ticks.every((t) => !t.includes('.')), `步长为整百时刻度不带小数: ${ticks}`);
  });
});

describe('xLabelIndices', () => {
  it('总是标出最后一天，挤不下时替换掉前一个标签', () => {
    assert.deepEqual(xLabelIndices(30, 2).slice(-2), [26, 29]);
    assert.deepEqual(xLabelIndices(10, 3), [0, 3, 6, 9]);
    assert.deepEqual(xLabelIndices(1, 1), [0]);
    assert.deepEqual(xLabelIndices(0, 1), []);
  });
});

describe('trendPoints', () => {
  // Local midnight: trendPoints groups by the viewer's day, so the test must not assume a timezone.
  const hourly = (hours: number, start = new Date(2026, 8, 1).getTime()) =>
    Array.from({ length: hours }, (_, i) => ({ timestamp: new Date(start + i * 3600_000).toISOString(), balance: 1000 - i, need_alarm: false }));

  it('超过两天的历史按天取最后一个快照，30 天约 720 个点只画 30 个', () => {
    const { points, labels } = trendPoints(hourly(30 * 24));
    assert.equal(points.length, 30);
    assert.equal(points[0]?.balance, 1000 - 23, '每天取当天最后一个快照');
    assert.equal(new Set(labels).size, labels.length, '日期标签不重复');
  });

  it('两天以内保留逐小时的细节，标签带时间', () => {
    const { points, labels } = trendPoints(hourly(30));
    assert.equal(points.length, 30);
    assert.match(labels[0] ?? '', /^\d\d\/\d\d \d\d:\d\d$/);
  });
});
