// Words for the Spend page: a month of money — what the meters burned
// ("metered") and which dated renewals fall in it ("fixed").
import { defineArea } from '../define';

export default defineArea({
  en: {
    // the month switcher
    'month.label': 'Month',
    'month.prev': 'Previous month',
    'month.next': 'Next month',
    'month.current': 'This month',

    // the headline
    'metered': 'Metered',
    'metered.hint': 'What the meters burned: balances going down and running totals going up. Only amounts in a currency are added up.',
    'fixed': 'Fixed',
    'fixed.hint': 'Dated renewals that fall in this month, paid or not.',
    'spent': 'Spent',
    'spentSoFar': 'Spent so far',
    'spent.day': 'Day {day} of {days}',
    'projected': 'Heading for',
    'projected.pace': 'at about {amount} a day from here',
    'renewals': 'Renewals',
    'renewals.count': '{n} payment | {n} payments',
    'renewals.countLeft': '{n} payment, {left} still to come | {n} payments, {left} still to come',
    'renewals.zero': 'None this month',
    'sum.current': 'With renewals, the month is heading for {amount}.',
    'sum.past': 'With renewals, the month came to {amount}.',
    'base': 'Totals in {base}.',
    'hero.expected': 'Expected this month',
    'hero.total': 'Spent this month',
    'hero.hint': 'What the meters spend by the end of the month at the pace they are going, and the renewals that fall in it. Each part is in the bar below.',

    // amounts left out of the totals
    'unconverted': 'Amounts in {list} are not in these totals: no exchange rate is set for them yet.',
    'unconverted.action': 'Set a rate',

    // the days of the month
    'daily.title': 'Metered spend per day',
    'daily.summary': '{total} metered in {month}; the most, {peak}, on {peakLabel}.',
    'daily.none': 'Nothing was metered in {month}.',
    'daily.col': 'Spend',

    // by meter
    'meters.title': 'By meter',
    'meters.caption': 'Spend by meter, {month}',
    'col.meter': 'Meter',
    'col.spent': 'Spent',
    'col.projected': 'Heading for',
    'col.budget': 'Budget',
    'row.inBase': 'about {amount}',
    'row.notCounted': 'Not in the total',
    'row.notCounted.hint': 'Only amounts in a currency are added up, and {unit} is not one.',
    'row.noRate': 'No exchange rate',
    'row.noRate.hint': 'There is no rate from {unit} to {base}, so this is left out of the total.',
    'row.budget': 'Budget {budget}',
    'row.over': 'Over budget',
    'row.headingOver': 'Heading over',
    'bar.budget': '{spent} of {budget} spent.',
    'bar.budgetProjected': '{spent} of {budget} spent; heading for {projected}.',
    'total': 'Total in {base}',

    // renewals
    'renewals.title': 'Renewals',
    'renewals.all': 'All dates',
    'renewals.when': 'When',
    'renewals.amount': 'Amount',

    // six months
    'history.title': 'Six months',
    'history.meta': 'metered, in {base}',
    'history.month': '{month}: {metered} metered, {fixed} in renewals',
    'history.heading': 'The lighter part is what this month is still expected to cost.',

    // a month with nothing in it
    'empty.title': 'Nothing in {month}',
    'empty.past': 'No meter recorded any spend in this month, and no renewal falls in it.',
    'empty.current': 'Spend shows up here once a balance or a running total has been read for a while, or a date carries an amount.',
    'empty.meters': 'Go to meters',
    'empty.dates': 'Go to dates',
  },
  zh: {
    'month.label': '月份',
    'month.prev': '上个月',
    'month.next': '下个月',
    'month.current': '回到本月',

    'metered': '按量',
    'metered.hint': '各项额度实际消耗掉的：余额减少的部分，加上累计增加的部分。只有货币金额才会加总。',
    'fixed': '固定',
    'fixed.hint': '落在这个月的续费，不论是否已经付过。',
    'spent': '已花',
    'spentSoFar': '已花',
    'spent.day': '本月第 {day} 天，共 {days} 天',
    'projected': '预计月底',
    'projected.pace': '往后按每天约 {amount} 估算',
    'renewals': '续费',
    'renewals.count': '{n} 笔',
    'renewals.countLeft': '{n} 笔，还有 {left} 笔没到',
    'renewals.zero': '这个月没有',
    'sum.current': '连同续费，这个月预计共 {amount}。',
    'sum.past': '连同续费，这个月共 {amount}。',
    'base': '合计按 {base} 计。',
    'hero.expected': '本月预计',
    'hero.total': '本月共花',
    'hero.hint': '按现在的速度，各项额度到月底会花掉的，加上落在本月的续费。下面的条按这三部分分开画。',

    'unconverted': '{list} 的金额没有计入合计：还没有设置对应的汇率。',
    'unconverted.action': '去设置汇率',

    'daily.title': '每日按量消耗',
    'daily.summary': '{month}按量消耗 {total}；最多的一天是 {peakLabel}，{peak}。',
    'daily.none': '{month}没有按量消耗。',
    'daily.col': '消耗',

    'meters.title': '各项额度',
    'meters.caption': '{month}各项额度的消耗',
    'col.meter': '额度',
    'col.spent': '已花',
    'col.projected': '预计月底',
    'col.budget': '预算',
    'row.inBase': '约合 {amount}',
    'row.notCounted': '不计入合计',
    'row.notCounted.hint': '只有货币金额才会加总，{unit} 不是货币。',
    'row.noRate': '没有汇率',
    'row.noRate.hint': '还没有 {unit} 兑 {base} 的汇率，所以没有计入合计。',
    'row.budget': '预算 {budget}',
    'row.over': '已超预算',
    'row.headingOver': '预计超支',
    'bar.budget': '预算 {budget}，已花 {spent}。',
    'bar.budgetProjected': '预算 {budget}，已花 {spent}，预计月底 {projected}。',
    'total': '合计（{base}）',

    'renewals.title': '续费',
    'renewals.all': '全部日期',
    'renewals.when': '时间',
    'renewals.amount': '金额',

    'history.title': '近六个月',
    'history.meta': '按量消耗 · {base}',
    'history.month': '{month}：按量 {metered}，续费 {fixed}',
    'history.heading': '浅色部分是这个月预计还会花的。',

    'empty.title': '{month}没有记录',
    'empty.past': '这个月没有额度产生消耗，也没有落在这个月的续费。',
    'empty.current': '余额类或累计类的额度读上一段时间，或者给某个日期填上金额之后，这里就会开始记账。',
    'empty.meters': '去看额度',
    'empty.dates': '去看日期',
  },
});
