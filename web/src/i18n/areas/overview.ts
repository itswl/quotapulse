// Words for the overview: the verdict, what needs attention, what is coming
// up, and the month in one line.
import { defineArea } from '../define';

export default defineArea({
  en: {
    // the verdict
    'verdict.clear': 'All clear',
    'verdict.attention': '{n} thing needs attention | {n} things need attention',
    'verdict.known': 'Nothing new',
    'verdict.known.note': '{n} acknowledged issue is still open | {n} acknowledged issues are still open',
    'verdict.plusKnown': 'and {n} acknowledged',
    'watching': 'Watching {meters} and {dates}',
    'watching.meters': '{n} meter | {n} meters',
    'watching.dates': '{n} date | {n} dates',
    'lastRead': 'Last read {ago}',
    'neverRead': 'Nothing has been read yet',
    'readDone': 'Everything was read again',

    // needs attention
    'attention.title': 'Needs attention',
    'attention.open': 'Look into it',

    // meters
    'meters.title': 'Meters',
    'meters.all': 'All {n}',

    // coming up
    'upcoming.title': 'Coming up',
    'upcoming.all': 'All dates',
    'upcoming.done': 'Mark “{title}” as done',
    'upcoming.doneToast': '“{title}” is done until next time',
    'upcoming.amount': 'Amount',

    // this month
    'month.title': 'This month',
    'month.spent': 'Spent so far',
    'month.spent.hint': 'What the meters have burned since the 1st.',
    'month.projected': 'Heading for',
    'month.projected.hint': 'Where metered spend ends the month at the expected daily burn.',
    'month.fixed': 'Renewals this month',
    'month.fixed.hint': 'Dated payments that fall in this month, paid or not.',
    'month.base': 'Totals in {base}.',
    'month.unconverted': 'Not counted: amounts in {list}, which have no exchange rate.',
    'month.all': 'Spend in detail',

    // first steps, on an empty install
    'start.title': 'Nothing is being watched yet',
    'start.body': 'Three steps, in any order, and QuotaPulse can start telling you things.',
    'start.account.title': 'Add an account',
    'start.account.body': 'Connect a platform you pay for. Its balance or quota shows up here within seconds.',
    'start.channel.title': 'Add a notification channel',
    'start.channel.body': 'Say where alerts should go: a chat group, your phone, a webhook.',
    'start.date.title': 'Add a date',
    'start.date.body': 'A renewal, a birthday, a domain that expires — anything that comes due.',
    'start.done': 'Done',

    // a standing hint
    'noChannel': 'There is no notification channel yet, so alerts have nowhere to go.',
    'noChannel.action': 'Add one',
  },
  zh: {
    'verdict.clear': '一切正常',
    'verdict.attention': '{n} 件事需要处理',
    'verdict.known': '没有新的问题',
    'verdict.known.note': '还有 {n} 个已知悉的问题没有解决',
    'verdict.plusKnown': '另有 {n} 个已知悉',
    'watching': '正在关注 {meters}和 {dates}',
    'watching.meters': '{n} 项额度',
    'watching.dates': '{n} 个日期',
    'lastRead': '{ago}读取过',
    'neverRead': '还没有读取过',
    'readDone': '已全部重新读取',

    'attention.title': '需要处理',
    'attention.open': '去看看',

    'meters.title': '额度',
    'meters.all': '全部 {n} 项',

    'upcoming.title': '即将到来',
    'upcoming.all': '全部日期',
    'upcoming.done': '把「{title}」标记为已完成',
    'upcoming.doneToast': '「{title}」这一次已完成',
    'upcoming.amount': '金额',

    'month.title': '本月',
    'month.spent': '已花',
    'month.spent.hint': '从 1 号到现在，各项额度消耗的总和。',
    'month.projected': '预计月底',
    'month.projected.hint': '按预计的日均消耗，到月底会花到多少。',
    'month.fixed': '本月续费',
    'month.fixed.hint': '落在本月的固定付款，不论是否已经付过。',
    'month.base': '合计按 {base} 计。',
    'month.unconverted': '未计入：{list} 还没有设置汇率。',
    'month.all': '花费明细',

    'start.title': '还没有要关注的东西',
    'start.body': '下面三步不分先后，做完之后 QuotaPulse 就能开始提醒你。',
    'start.account.title': '添加一个账户',
    'start.account.body': '接入一个你在付费的平台，几秒钟后它的余额或配额就会出现在这里。',
    'start.channel.title': '添加一个通知渠道',
    'start.channel.body': '告诉它提醒该发到哪里：群聊、手机，或者一个 Webhook。',
    'start.date.title': '添加一个日期',
    'start.date.body': '续费、生日、域名到期——任何会到期的日子。',
    'start.done': '已完成',

    'noChannel': '还没有通知渠道，提醒发不出去。',
    'noChannel.action': '去添加',
  },
});
