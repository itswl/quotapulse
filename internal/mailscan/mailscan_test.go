package mailscan

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/notify"
	"github.com/itswl/quotapulse/internal/store"
)

// fakeStore cares only about the two mail-alert methods; the rest come from the Null store.
type fakeStore struct {
	store.Store
	mu      sync.Mutex
	recent  bool
	err     error
	queries []emailQuery
	saved   []store.EmailAlertRecord
}

type emailQuery struct {
	mailbox string
	sender  string
	subject string
	date    string
	days    int
}

func newFakeStore() *fakeStore { return &fakeStore{Store: store.Null()} }

func (f *fakeStore) HasRecentEmailAlert(_ context.Context, mailbox, sender, subject, date string, days int) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, emailQuery{mailbox, sender, subject, date, days})
	return f.recent, f.err
}

func (f *fakeStore) SaveEmailAlert(_ context.Context, rec store.EmailAlertRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, rec)
	return nil
}

type fakeNotifier struct {
	mu   sync.Mutex
	err  error
	sent []notify.Message
}

func (f *fakeNotifier) Send(_ context.Context, msg notify.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, msg)
	return f.err
}

func (f *fakeNotifier) messages() []notify.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]notify.Message(nil), f.sent...)
}

// fakeConn impersonates a logged-in mailbox; IMAP protocol details stay out of tests.
type fakeConn struct {
	mu        sync.Mutex
	order     []uint32
	raws      map[uint32][]byte
	searchErr error
	failBatch bool // 批量取整批失败，用来测降级为逐封获取
	since     time.Time
	fetched   [][]uint32
	closed    bool
}

func newFakeConn(mails ...[]byte) *fakeConn {
	c := &fakeConn{raws: map[uint32][]byte{}}
	for i, mail := range mails {
		num := uint32(i + 1)
		c.order = append(c.order, num)
		c.raws[num] = mail
	}
	return c
}

func (c *fakeConn) Search(since time.Time) ([]uint32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.since = since
	if c.searchErr != nil {
		return nil, c.searchErr
	}
	return append([]uint32(nil), c.order...), nil
}

func (c *fakeConn) Fetch(nums []uint32) ([][]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fetched = append(c.fetched, append([]uint32(nil), nums...))
	if c.failBatch && len(nums) > 1 {
		return nil, errors.New("批量 FETCH 失败")
	}
	raws := make([][]byte, 0, len(nums))
	for _, num := range nums {
		if raw, ok := c.raws[num]; ok {
			raws = append(raws, raw)
		}
	}
	return raws, nil
}

func (c *fakeConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func (c *fakeConn) fetchedNums() []uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	var all []uint32
	for _, batch := range c.fetched {
		all = append(all, batch...)
	}
	return all
}

// harness assembles the scanner with all its dependencies, dispatching connections by mailbox name.
type harness struct {
	scanner  *Scanner
	store    *fakeStore
	notifier *fakeNotifier
	conns    map[string]*fakeConn
	dialErrs map[string]error

	mu     sync.Mutex
	dialed []string
}

func newHarness(conns map[string]*fakeConn) *harness {
	h := &harness{
		store:    newFakeStore(),
		notifier: &fakeNotifier{},
		conns:    conns,
		dialErrs: map[string]error{},
	}
	h.scanner = &Scanner{
		Store:    h.store,
		Notifier: h.notifier,
		Log:      slog.New(slog.DiscardHandler),
		// retry waits compressed to the minimum, or one unreachable mailbox would cost the test 8 seconds
		retryWait: time.Microsecond,
		dial:      h.dial,
	}
	return h
}

func (h *harness) dial(_ context.Context, m model.Mailbox, _ time.Duration) (mailConn, error) {
	name := displayName(m)
	h.mu.Lock()
	h.dialed = append(h.dialed, name)
	h.mu.Unlock()

	if err := h.dialErrs[name]; err != nil {
		return nil, err
	}
	conn, ok := h.conns[name]
	if !ok {
		return nil, fmt.Errorf("测试没为邮箱 %s 准备连接", name)
	}
	return conn, nil
}

func (h *harness) dialCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.dialed)
}

// alertMail is a typical arrears reminder shaped like a real billing mail:
// subject 【阿里云】余额不足提醒, body 您的账户余额不足，请及时充值。余额：12.50元
func alertMail(messageID string) []byte {
	return rawMessage([]string{
		"Subject: =?utf-8?B?44CQ6Zi/6YeM5LqR44CR5L2Z6aKd5LiN6Laz5o+Q6YaS?=",
		"From: noreply@aliyun.com",
		"Date: Mon, 01 Sep 2026 10:00:00 +0800",
		"Message-ID: " + messageID,
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: base64",
	}, "5oKo55qE6LSm5oi35L2Z6aKd5LiN6Laz77yM6K+35Y+K5pe25YWF5YC844CC5L2Z6aKd77yaMTIuNTDlhYM=")
}

func normalMail(messageID string) []byte {
	return rawMessage([]string{
		"Subject: Weekly report",
		"From: boss@example.com",
		"Date: Mon, 01 Sep 2026 09:00:00 +0800",
		"Message-ID: " + messageID,
		"Content-Type: text/plain; charset=utf-8",
	}, "周报请查收")
}

func mailbox(name, host string) model.Mailbox {
	return model.Mailbox{
		Name: name, Host: host, Port: 993, UseSSL: true, Enabled: true,
		Username: name + "@example.com", Password: "secret",
	}
}

// ---------- Test cases ----------

func TestScanWithoutMailboxes(t *testing.T) {
	h := newHarness(nil)
	result := h.scanner.Scan(context.Background(), nil, 3, true)

	if result.Days != 3 || !result.DryRun {
		t.Errorf("结果 = %+v, 期望原样带回 days 与 dry_run", result)
	}
	if len(result.Mailboxes) != 0 || len(result.Alerts) != 0 {
		t.Errorf("结果 = %+v, 期望空", result)
	}
	// serialized JSON must be [], not null — the frontend breaks on null
	if result.Mailboxes == nil || result.Alerts == nil {
		t.Error("空结果也要是空切片，不能是 nil")
	}
}

func TestScanDryRunFindsAlertWithoutSending(t *testing.T) {
	conn := newFakeConn(alertMail("<alert-1@aliyun.com>"), normalMail("<weekly-1@example.com>"))
	h := newHarness(map[string]*fakeConn{"A": conn})

	result := h.scanner.Scan(context.Background(), []model.Mailbox{mailbox("A", "imap.a.com")}, 1, true)

	if len(result.Mailboxes) != 1 {
		t.Fatalf("邮箱结果数 = %d", len(result.Mailboxes))
	}
	box := result.Mailboxes[0]
	if !box.Success || box.TotalEmails != 2 || box.AlertCount != 1 {
		t.Errorf("邮箱统计 = %+v, 期望 2 封邮件、1 封告警", box)
	}
	if box.Host != "imap.a.com" || box.Port != 993 || box.Username != "A@example.com" {
		t.Errorf("邮箱统计 = %+v, 期望原样带上连接信息", box)
	}

	if len(result.Alerts) != 1 {
		t.Fatalf("告警数 = %d", len(result.Alerts))
	}
	alert := result.Alerts[0]
	if alert.Subject != "【阿里云】余额不足提醒" {
		t.Errorf("主题 = %q, MIME 头没解对", alert.Subject)
	}
	if alert.ServiceName == nil || *alert.ServiceName != "阿里云" {
		t.Errorf("服务名 = %v", alert.ServiceName)
	}
	if alert.Amount == nil || *alert.Amount != 12.5 {
		t.Errorf("金额 = %v, 期望 12.5", alert.Amount)
	}
	if !strings.Contains(strings.Join(alert.Keywords, ","), "余额不足") {
		t.Errorf("关键词 = %v", alert.Keywords)
	}
	if alert.AlertSent {
		t.Error("测试模式不该发通知")
	}

	// dry-run neither dedupes nor records: a trial run must not mark a mail "handled"
	if len(h.store.queries) != 0 || len(h.store.saved) != 0 {
		t.Errorf("测试模式动了数据库: queries=%d saved=%d", len(h.store.queries), len(h.store.saved))
	}
	if len(h.notifier.messages()) != 0 {
		t.Error("测试模式不该发通知")
	}
	if !conn.closed {
		t.Error("扫完要断开连接")
	}
}

func TestScanSendsAlertAndRecordsIt(t *testing.T) {
	h := newHarness(map[string]*fakeConn{"A": newFakeConn(alertMail("<alert-1@aliyun.com>"))})
	var notified []string
	h.scanner.OnNotify = func(kind string, ok bool) {
		notified = append(notified, fmt.Sprintf("%s=%t", kind, ok))
	}

	result := h.scanner.Scan(context.Background(), []model.Mailbox{mailbox("A", "imap.a.com")}, 1, false)

	if len(result.Alerts) != 1 || !result.Alerts[0].AlertSent {
		t.Fatalf("告警 = %+v, 期望已发送", result.Alerts)
	}
	msgs := h.notifier.messages()
	if len(msgs) != 1 || msgs[0].Kind != notify.KindEmail {
		t.Fatalf("通知 = %+v, 期望一条 email 类通知", msgs)
	}
	if !strings.Contains(msgs[0].Title, "【阿里云】余额不足提醒") {
		t.Errorf("通知标题 = %q", msgs[0].Title)
	}
	if want := []string{"email=true"}; len(notified) != 1 || notified[0] != want[0] {
		t.Errorf("OnNotify 回调 = %v, 期望 %v", notified, want)
	}

	if len(h.store.saved) != 1 {
		t.Fatalf("留痕 = %+v, 期望一条", h.store.saved)
	}
	rec := h.store.saved[0]
	if rec.Mailbox != "A" || !rec.AlertSent || rec.Amount == nil || *rec.Amount != 12.5 {
		t.Errorf("留痕 = %+v", rec)
	}
	if rec.ServiceName == nil || *rec.ServiceName != "阿里云" || len(rec.Keywords) == 0 {
		t.Errorf("留痕 = %+v, 期望带上服务名与关键词", rec)
	}
}

// Failed sends are recorded too: next time we know the mail was processed and did not go out.
func TestScanRecordsFailedNotification(t *testing.T) {
	h := newHarness(map[string]*fakeConn{"A": newFakeConn(alertMail("<alert-1@aliyun.com>"))})
	h.notifier.err = errors.New("webhook 挂了")
	var notified []bool
	h.scanner.OnNotify = func(_ string, ok bool) { notified = append(notified, ok) }

	result := h.scanner.Scan(context.Background(), []model.Mailbox{mailbox("A", "imap.a.com")}, 1, false)

	if len(result.Alerts) != 1 || result.Alerts[0].AlertSent {
		t.Fatalf("告警 = %+v, 期望标成没发出去", result.Alerts)
	}
	if len(h.store.saved) != 1 || h.store.saved[0].AlertSent {
		t.Errorf("留痕 = %+v, 期望记下发送失败", h.store.saved)
	}
	if len(notified) != 1 || notified[0] {
		t.Errorf("OnNotify 回调 = %v, 期望报告失败", notified)
	}
}

func TestScanSkipsRecentDuplicate(t *testing.T) {
	h := newHarness(map[string]*fakeConn{"A": newFakeConn(alertMail("<alert-1@aliyun.com>"))})
	h.store.recent = true

	result := h.scanner.Scan(context.Background(), []model.Mailbox{mailbox("A", "imap.a.com")}, 3, false)

	if len(result.Alerts) != 1 || result.Alerts[0].AlertSent {
		t.Fatalf("告警 = %+v, 期望命中去重后不再发送", result.Alerts)
	}
	if len(h.notifier.messages()) != 0 {
		t.Error("命中去重还发了通知")
	}
	if len(h.store.saved) != 0 {
		t.Error("命中去重不该再留痕")
	}
	if len(h.store.queries) != 1 {
		t.Fatalf("去重查询 = %+v", h.store.queries)
	}
	q := h.store.queries[0]
	if q.days != 3 || q.mailbox != "A" || q.subject != "【阿里云】余额不足提醒" {
		t.Errorf("去重查询 = %+v, 期望按邮箱/发件人/主题/日期查最近 3 天", q)
	}
	if !strings.Contains(q.sender, "noreply@aliyun.com") {
		t.Errorf("去重查询的发件人 = %q", q.sender)
	}
}

// When the database misbehaves, prefer a duplicate alert over swallowing the arrears warning.
func TestScanSendsWhenDedupeQueryFails(t *testing.T) {
	h := newHarness(map[string]*fakeConn{"A": newFakeConn(alertMail("<alert-1@aliyun.com>"))})
	h.store.err = errors.New("数据库连不上")

	result := h.scanner.Scan(context.Background(), []model.Mailbox{mailbox("A", "imap.a.com")}, 1, false)

	if len(result.Alerts) != 1 || !result.Alerts[0].AlertSent {
		t.Fatalf("告警 = %+v, 期望照发", result.Alerts)
	}
}

// One notification is often cc'd to several scanned mailboxes; within one scan it should alert once.
func TestScanDedupesSameMessageAcrossMailboxes(t *testing.T) {
	h := newHarness(map[string]*fakeConn{
		"A": newFakeConn(alertMail("<alert-1@aliyun.com>")),
		"B": newFakeConn(alertMail("<alert-1@aliyun.com>")),
	})

	result := h.scanner.Scan(context.Background(),
		[]model.Mailbox{mailbox("A", "imap.a.com"), mailbox("B", "imap.b.com")}, 1, true)

	if len(result.Alerts) != 1 {
		t.Fatalf("告警数 = %d, 期望同一封邮件只处理一次", len(result.Alerts))
	}
	total := result.Mailboxes[0].AlertCount + result.Mailboxes[1].AlertCount
	if total != 1 {
		t.Errorf("逐邮箱告警数 = %d + %d, 期望合计 1",
			result.Mailboxes[0].AlertCount, result.Mailboxes[1].AlertCount)
	}
	if result.Mailboxes[0].TotalEmails != 1 || result.Mailboxes[1].TotalEmails != 1 {
		t.Error("去重不该影响各邮箱扫到的邮件总数")
	}
}

// Every scan starts fresh; last round's mails must match again this round.
func TestScanResetsSeenBetweenScans(t *testing.T) {
	h := newHarness(map[string]*fakeConn{"A": newFakeConn(alertMail("<alert-1@aliyun.com>"))})
	boxes := []model.Mailbox{mailbox("A", "imap.a.com")}

	first := h.scanner.Scan(context.Background(), boxes, 1, true)
	second := h.scanner.Scan(context.Background(), boxes, 1, true)

	if len(first.Alerts) != 1 || len(second.Alerts) != 1 {
		t.Errorf("两轮告警数 = %d / %d, 期望各 1", len(first.Alerts), len(second.Alerts))
	}
}

func TestScanIncompleteMailboxConfig(t *testing.T) {
	h := newHarness(nil)
	broken := model.Mailbox{Name: "bad", Host: "imap.x.com", Enabled: true}

	result := h.scanner.Scan(context.Background(), []model.Mailbox{broken}, 1, false)

	box := result.Mailboxes[0]
	if box.Success || box.Error == nil || !strings.Contains(*box.Error, "Incomplete configuration") {
		t.Errorf("邮箱结果 = %+v, 期望记下配置不完整", box)
	}
	if h.dialCount() != 0 {
		t.Error("配置不完整不该去连服务器")
	}
	// a config error is not an incident alert: note it in the result without paging the group
	if len(h.notifier.messages()) != 0 {
		t.Error("配置不完整不该发系统告警")
	}
}

func TestScanConnectionFailure(t *testing.T) {
	h := newHarness(map[string]*fakeConn{})
	h.dialErrs["A"] = errors.New("login failed")
	boxes := []model.Mailbox{mailbox("A", "imap.a.com")}

	result := h.scanner.Scan(context.Background(), boxes, 1, false)

	box := result.Mailboxes[0]
	if box.Success || box.Error == nil || !strings.Contains(*box.Error, "login failed") {
		t.Errorf("邮箱结果 = %+v, 期望记下连接错误", box)
	}
	// an unreachable mailbox must exhaust connectAttempts, not give up after one try
	if got := h.dialCount(); got != connectAttempts {
		t.Errorf("连接尝试 = %d 次, 期望 %d 次", got, connectAttempts)
	}
	msgs := h.notifier.messages()
	if len(msgs) != 1 || msgs[0].Kind != notify.KindMailboxError {
		t.Fatalf("通知 = %+v, 期望一条 mailbox_error", msgs)
	}

	// in dry-run the same failure does not disturb the on-call group
	h2 := newHarness(map[string]*fakeConn{})
	h2.dialErrs["A"] = errors.New("login failed")
	h2.scanner.Scan(context.Background(), boxes, 1, true)
	if len(h2.notifier.messages()) != 0 {
		t.Error("测试模式不该发系统告警")
	}
}

// One unreachable mailbox must not stop the others — the main reason scanning is concurrent.
func TestScanIsolatesFailingMailbox(t *testing.T) {
	h := newHarness(map[string]*fakeConn{"B": newFakeConn(alertMail("<alert-2@aliyun.com>"))})
	h.dialErrs["A"] = errors.New("connection refused")

	result := h.scanner.Scan(context.Background(),
		[]model.Mailbox{mailbox("A", "imap.a.com"), mailbox("B", "imap.b.com")}, 1, true)

	if result.Mailboxes[0].Success || !result.Mailboxes[1].Success {
		t.Errorf("邮箱结果 = %+v", result.Mailboxes)
	}
	if len(result.Alerts) != 1 || result.Alerts[0].Mailbox != "B" {
		t.Errorf("告警 = %+v, 期望 B 的告警照常产出", result.Alerts)
	}
}

func TestScanSearchFailure(t *testing.T) {
	conn := newFakeConn(alertMail("<alert-1@aliyun.com>"))
	conn.searchErr = errors.New("SEARCH 被拒绝")
	h := newHarness(map[string]*fakeConn{"A": conn})

	result := h.scanner.Scan(context.Background(), []model.Mailbox{mailbox("A", "imap.a.com")}, 1, true)

	box := result.Mailboxes[0]
	if box.Success || box.Error == nil || !strings.Contains(*box.Error, "SEARCH") {
		t.Errorf("邮箱结果 = %+v", box)
	}
	if !conn.closed {
		t.Error("搜索失败也要断开连接")
	}
}

// When a bulk FETCH fails, degrade to per-mail fetches: one bad mail must not cost the other 99 in the batch.
func TestScanFallsBackToSequentialFetch(t *testing.T) {
	conn := newFakeConn(normalMail("<weekly-1@example.com>"), alertMail("<alert-1@aliyun.com>"))
	conn.failBatch = true
	h := newHarness(map[string]*fakeConn{"A": conn})

	result := h.scanner.Scan(context.Background(), []model.Mailbox{mailbox("A", "imap.a.com")}, 1, true)

	if len(result.Alerts) != 1 {
		t.Fatalf("告警数 = %d, 降级取件后仍应扫到告警", len(result.Alerts))
	}
	if got := len(conn.fetched); got != 3 { // 1 次批量 + 2 次逐封
		t.Errorf("FETCH 次数 = %d, 期望 3", got)
	}
}

func TestScanRespectsMaxEmails(t *testing.T) {
	conn := newFakeConn(
		normalMail("<n1@example.com>"), normalMail("<n2@example.com>"),
		normalMail("<n3@example.com>"), alertMail("<alert-1@aliyun.com>"),
	)
	h := newHarness(map[string]*fakeConn{"A": conn})
	h.scanner.MaxEmails = 2

	result := h.scanner.Scan(context.Background(), []model.Mailbox{mailbox("A", "imap.a.com")}, 1, true)

	if result.Mailboxes[0].TotalEmails != 2 {
		t.Errorf("扫描邮件数 = %d, 期望被上限截到 2", result.Mailboxes[0].TotalEmails)
	}
	// the two newest (highest sequence numbers) were cut; the last alert mail must still be there
	if want := []uint32{3, 4}; fmt.Sprint(conn.fetchedNums()) != fmt.Sprint(want) {
		t.Errorf("取回的序号 = %v, 期望最新的 %v", conn.fetchedNums(), want)
	}
	if len(result.Alerts) != 1 {
		t.Errorf("告警数 = %d", len(result.Alerts))
	}
}

// With more mailboxes than the concurrency cap, results still come back in input order so page cards do not jump around.
func TestScanKeepsMailboxOrder(t *testing.T) {
	const count = 8
	conns := map[string]*fakeConn{}
	var boxes []model.Mailbox
	for i := range count {
		name := fmt.Sprintf("box-%d", i)
		conns[name] = newFakeConn(alertMail(fmt.Sprintf("<alert-%d@aliyun.com>", i)))
		boxes = append(boxes, mailbox(name, fmt.Sprintf("imap.%d.com", i)))
	}
	h := newHarness(conns)

	result := h.scanner.Scan(context.Background(), boxes, 1, true)

	if len(result.Mailboxes) != count || len(result.Alerts) != count {
		t.Fatalf("结果数 = %d 邮箱 / %d 告警, 期望各 %d",
			len(result.Mailboxes), len(result.Alerts), count)
	}
	for i := range count {
		want := fmt.Sprintf("box-%d", i)
		if result.Mailboxes[i].Name != want {
			t.Errorf("第 %d 个邮箱 = %q, 期望 %q", i, result.Mailboxes[i].Name, want)
		}
		if result.Alerts[i].Mailbox != want {
			t.Errorf("第 %d 条告警来自 %q, 期望 %q", i, result.Alerts[i].Mailbox, want)
		}
	}
}

func TestScanUsesConfiguredKeywords(t *testing.T) {
	h := newHarness(map[string]*fakeConn{"A": newFakeConn(alertMail("<alert-1@aliyun.com>"))})
	h.scanner.Keywords = []string{"配额不足"}

	result := h.scanner.Scan(context.Background(), []model.Mailbox{mailbox("A", "imap.a.com")}, 1, true)
	if len(result.Alerts) != 0 {
		t.Errorf("告警数 = %d, 自定义词表应当整体替换默认词表", len(result.Alerts))
	}

	h.scanner.Keywords = []string{"余额不足"}
	if result = h.scanner.Scan(context.Background(), []model.Mailbox{mailbox("A", "imap.a.com")}, 1, true); len(result.Alerts) != 1 {
		t.Errorf("告警数 = %d, 期望 1", len(result.Alerts))
	}
}

func TestScanSearchesSinceRequestedDays(t *testing.T) {
	conn := newFakeConn()
	h := newHarness(map[string]*fakeConn{"A": conn})

	h.scanner.Scan(context.Background(), []model.Mailbox{mailbox("A", "imap.a.com")}, 7, true)

	want := time.Now().AddDate(0, 0, -7)
	if diff := conn.since.Sub(want); diff > time.Minute || diff < -time.Minute {
		t.Errorf("SINCE = %v, 期望约等于 %v", conn.since, want)
	}
}

func TestSeenSetMarksOnce(t *testing.T) {
	var seen seenSet
	var wg sync.WaitGroup
	results := make([]bool, 50)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = seen.mark("<same@example.com>")
		}()
	}
	wg.Wait()

	marked := 0
	for _, ok := range results {
		if ok {
			marked++
		}
	}
	if marked != 1 {
		t.Errorf("mark 返回 true 的次数 = %d, 期望 1", marked)
	}
}

func TestDisplayName(t *testing.T) {
	tests := []struct {
		mailbox model.Mailbox
		want    string
	}{
		{model.Mailbox{Name: "工作邮箱", Username: "u@x.com"}, "工作邮箱"},
		{model.Mailbox{Username: "u@x.com"}, "u@x.com"},
		{model.Mailbox{}, "(Unnamed)"},
	}
	for _, tt := range tests {
		if got := displayName(tt.mailbox); got != tt.want {
			t.Errorf("displayName(%+v) = %q, 期望 %q", tt.mailbox, got, tt.want)
		}
	}
}

func TestMaxEmailsFallback(t *testing.T) {
	tests := []struct {
		configured int
		want       int
	}{
		{0, defaultMaxEmails}, // 没配置就是没配置，不是"只扫 0 封"
		{-5, 1},
		{50, 50},
	}
	for _, tt := range tests {
		if got := (&Scanner{MaxEmails: tt.configured}).maxEmails(); got != tt.want {
			t.Errorf("MaxEmails=%d 时上限 = %d, 期望 %d", tt.configured, got, tt.want)
		}
	}
}

func TestMailboxPortDefaults(t *testing.T) {
	if got := mailboxPort(model.Mailbox{}); got != defaultPort {
		t.Errorf("端口 = %d, 期望默认 %d", got, defaultPort)
	}
	if got := mailboxPort(model.Mailbox{Port: 143}); got != 143 {
		t.Errorf("端口 = %d, 期望 143", got)
	}
}
