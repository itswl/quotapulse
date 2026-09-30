package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/model"
)

// Implementation note.
var allTables = []string{
	"balance_history",
	"alert_history",
	"project_config",
	"subscription_config",
	"email_config",
	"email_alert_history",
}

type backend struct {
	name string
	// Implementation note.
	dsn func(t *testing.T) string
}

// Implementation note.
//
// Implementation note.
// Implementation note.
// Implementation note.
func testBackends() []backend {
	backends := []backend{{
		name: "sqlite",
		dsn: func(t *testing.T) string {
			// Implementation note.
			return "sqlite:///" + filepath.Join(t.TempDir(), "store_test.db")
		},
	}}
	if url := os.Getenv("STORE_TEST_POSTGRES_URL"); url != "" {
		backends = append(backends, backend{name: "postgres", dsn: func(*testing.T) string { return url }})
	}
	if url := os.Getenv("STORE_TEST_MYSQL_URL"); url != "" {
		backends = append(backends, backend{name: "mysql", dsn: func(*testing.T) string { return url }})
	}
	return backends
}

// Implementation note.
// Implementation note.
type fixture struct {
	t   *testing.T
	url string
}

func newFixture(t *testing.T, b backend) *fixture {
	t.Helper()
	f := &fixture{t: t, url: b.dsn(t)}
	f.open(Options{}) // 先建表，再清空：共用的服务器上可能留着上一个用例的数据
	s := f.open(Options{})
	for _, table := range allTables {
		if _, err := s.db.ExecContext(t.Context(), "DELETE FROM "+table); err != nil {
			t.Fatalf("清空 %s 失败: %v", table, err)
		}
	}
	return f
}

func (f *fixture) open(opts Options) *sqlStore {
	f.t.Helper()
	opts.DatabaseURL = f.url
	s, err := Open(f.t.Context(), opts)
	if err != nil {
		f.t.Fatalf("打开数据库失败: %v", err)
	}
	f.t.Cleanup(func() { s.Close() })
	return s.(*sqlStore)
}

// Implementation note.
func eachBackend(t *testing.T, name string, run func(t *testing.T, f *fixture)) {
	t.Helper()
	for _, b := range testBackends() {
		t.Run(name+"/"+b.name, func(t *testing.T) {
			run(t, newFixture(t, b))
		})
	}
}

// Implementation note.

func TestProjectConfigCRUD(t *testing.T) {
	eachBackend(t, "project", func(t *testing.T, f *fixture) {
		s := f.open(Options{})
		ctx := t.Context()

		got, err := s.ListProjects(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Fatalf("空库应当没有项目，却有 %d 条", len(got))
		}

		owner := "平台组"
		project := model.Project{
			Name:         "生产-openrouter",
			Provider:     "openrouter",
			APIKey:       "sk-or-v1-abcdef",
			Threshold:    25.5,
			Type:         model.TypeCredits,
			OwnerProject: &owner,
			Enabled:      true,
		}
		if err := s.UpsertProject(ctx, project); err != nil {
			t.Fatal(err)
		}

		got, err = s.ListProjects(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Fatalf("期望 1 条项目，得到 %d 条", len(got))
		}
		if got[0] != project {
			// Implementation note.
			if got[0].Name != project.Name || got[0].Provider != project.Provider ||
				got[0].APIKey != project.APIKey || got[0].Threshold != project.Threshold ||
				got[0].Type != project.Type || got[0].Enabled != project.Enabled {
				t.Errorf("读回的项目是 %+v，期望 %+v", got[0], project)
			}
		}
		if got[0].OwnerProject == nil || *got[0].OwnerProject != owner {
			t.Errorf("owner_project = %v，期望 %q", got[0].OwnerProject, owner)
		}

		// Implementation note.
		project.Threshold = 99
		project.Enabled = false
		project.OwnerProject = nil
		if err := s.UpsertProject(ctx, project); err != nil {
			t.Fatal(err)
		}
		got, err = s.ListProjects(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Fatalf("同名 upsert 之后应当仍是 1 条，得到 %d 条", len(got))
		}
		if got[0].Threshold != 99 || got[0].Enabled {
			t.Errorf("覆盖写没生效: %+v", got[0])
		}
		if got[0].OwnerProject != nil {
			t.Errorf("owner_project 应当被清成 nil，得到 %v", *got[0].OwnerProject)
		}

		if err := s.DeleteProject(ctx, project.Name); err != nil {
			t.Fatal(err)
		}
		got, err = s.ListProjects(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Fatalf("删除后应当没有项目，却有 %d 条", len(got))
		}
		// Implementation note.
		if err := s.DeleteProject(ctx, "根本不存在"); err != nil {
			t.Errorf("删除不存在的项目不该报错: %v", err)
		}
	})
}

func TestSubscriptionConfigCRUD(t *testing.T) {
	eachBackend(t, "subscription", func(t *testing.T, f *fixture) {
		s := f.open(Options{})
		ctx := t.Context()

		renewed := "2026-09-01"
		sub := model.Subscription{
			Name:            "Claude Max 订阅",
			CycleType:       model.CycleMonthly,
			RenewalDay:      15,
			AlertDaysBefore: 3,
			Amount:          200,
			Enabled:         true,
			LastRenewedDate: &renewed,
		}
		if err := s.UpsertSubscription(ctx, sub); err != nil {
			t.Fatal(err)
		}

		got, err := s.ListSubscriptions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Fatalf("期望 1 条订阅，得到 %d 条", len(got))
		}
		if got[0].Name != sub.Name || got[0].CycleType != sub.CycleType ||
			got[0].RenewalDay != sub.RenewalDay || got[0].AlertDaysBefore != sub.AlertDaysBefore ||
			got[0].Amount != sub.Amount || !got[0].Enabled {
			t.Errorf("读回的订阅是 %+v，期望 %+v", got[0], sub)
		}
		if got[0].LastRenewedDate == nil || *got[0].LastRenewedDate != renewed {
			t.Errorf("last_renewed_date = %v，期望 %q", got[0].LastRenewedDate, renewed)
		}

		sub.LastRenewedDate = nil
		sub.RenewalDay = 28
		if err := s.UpsertSubscription(ctx, sub); err != nil {
			t.Fatal(err)
		}
		got, _ = s.ListSubscriptions(ctx)
		if len(got) != 1 || got[0].RenewalDay != 28 || got[0].LastRenewedDate != nil {
			t.Errorf("覆盖写没生效: %+v", got[0])
		}

		if err := s.DeleteSubscription(ctx, sub.Name); err != nil {
			t.Fatal(err)
		}
		if got, _ = s.ListSubscriptions(ctx); len(got) != 0 {
			t.Fatalf("删除后应当没有订阅，却有 %d 条", len(got))
		}
	})
}

func TestMailboxConfigCRUD(t *testing.T) {
	eachBackend(t, "mailbox", func(t *testing.T, f *fixture) {
		s := f.open(Options{})
		ctx := t.Context()

		box := model.Mailbox{
			Name:     "账单邮箱",
			Host:     "imap.example.com",
			Port:     993,
			Username: "billing@example.com",
			Password: "app-specific-password",
			UseSSL:   true,
			Enabled:  true,
		}
		if err := s.UpsertMailbox(ctx, box); err != nil {
			t.Fatal(err)
		}

		got, err := s.ListMailboxes(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Fatalf("期望 1 个邮箱，得到 %d 个", len(got))
		}
		if got[0] != box {
			t.Errorf("读回的邮箱是 %+v，期望 %+v", got[0], box)
		}

		box.Port = 143
		box.UseSSL = false
		if err := s.UpsertMailbox(ctx, box); err != nil {
			t.Fatal(err)
		}
		got, _ = s.ListMailboxes(ctx)
		if len(got) != 1 || got[0].Port != 143 || got[0].UseSSL {
			t.Errorf("覆盖写没生效: %+v", got[0])
		}

		if err := s.DeleteMailbox(ctx, box.Name); err != nil {
			t.Fatal(err)
		}
		if got, _ = s.ListMailboxes(ctx); len(got) != 0 {
			t.Fatalf("删除后应当没有邮箱，却有 %d 个", len(got))
		}
	})
}

// Implementation note.

func TestSecretsAreEncryptedAtRest(t *testing.T) {
	eachBackend(t, "encrypt-at-rest", func(t *testing.T, f *fixture) {
		ctx := t.Context()
		const apiKey = "sk-or-v1-super-secret"
		const password = "mailbox-secret"

		s := f.open(Options{EncryptionKey: sampleFernetKey})
		if err := s.UpsertProject(ctx, model.Project{
			Name: "p1", Provider: "openrouter", APIKey: apiKey, Enabled: true,
		}); err != nil {
			t.Fatal(err)
		}
		if err := s.UpsertMailbox(ctx, model.Mailbox{
			Name: "m1", Host: "h", Port: 993, Username: "u", Password: password, Enabled: true,
		}); err != nil {
			t.Fatal(err)
		}

		// Implementation note.
		projects, err := s.ListProjects(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(projects) != 1 || projects[0].APIKey != apiKey {
			t.Fatalf("带密钥读回的 api_key = %q，期望 %q", projects[0].APIKey, apiKey)
		}
		boxes, err := s.ListMailboxes(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(boxes) != 1 || boxes[0].Password != password {
			t.Fatalf("带密钥读回的 password = %q，期望 %q", boxes[0].Password, password)
		}

		// Implementation note.
		plain := f.open(Options{})
		stored, err := plain.ListProjects(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !isEncrypted(stored[0].APIKey) {
			t.Errorf("api_key 没有加密落盘: %q", stored[0].APIKey)
		}
		storedBoxes, err := plain.ListMailboxes(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !isEncrypted(storedBoxes[0].Password) {
			t.Errorf("password 没有加密落盘: %q", storedBoxes[0].Password)
		}
	})
}

func TestAutoEncryptOnRead(t *testing.T) {
	eachBackend(t, "auto-encrypt", func(t *testing.T, f *fixture) {
		ctx := t.Context()
		const apiKey = "legacy-plaintext-key"
		const password = "legacy-plaintext-password"

		// Implementation note.
		legacy := f.open(Options{})
		if err := legacy.UpsertProject(ctx, model.Project{
			Name: "p1", Provider: "openrouter", APIKey: apiKey, Enabled: true,
		}); err != nil {
			t.Fatal(err)
		}
		if err := legacy.UpsertMailbox(ctx, model.Mailbox{
			Name: "m1", Host: "h", Port: 993, Username: "u", Password: password, Enabled: true,
		}); err != nil {
			t.Fatal(err)
		}

		// Implementation note.
		s := f.open(Options{EncryptionKey: sampleFernetKey, AutoEncryptOnRead: true})
		projects, err := s.ListProjects(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if projects[0].APIKey != apiKey {
			t.Errorf("回写之后读到的仍应是明文 %q，得到 %q", apiKey, projects[0].APIKey)
		}
		if _, err := s.ListMailboxes(ctx); err != nil {
			t.Fatal(err)
		}

		stored := f.open(Options{})
		raw, err := stored.ListProjects(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !isEncrypted(raw[0].APIKey) {
			t.Errorf("api_key 应当已被回写成密文，实际是 %q", raw[0].APIKey)
		}
		rawBoxes, err := stored.ListMailboxes(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !isEncrypted(rawBoxes[0].Password) {
			t.Errorf("password 应当已被回写成密文，实际是 %q", rawBoxes[0].Password)
		}
	})
}

func TestAutoEncryptOnReadDisabled(t *testing.T) {
	eachBackend(t, "auto-encrypt-off", func(t *testing.T, f *fixture) {
		ctx := t.Context()
		const apiKey = "legacy-plaintext-key"

		legacy := f.open(Options{})
		if err := legacy.UpsertProject(ctx, model.Project{
			Name: "p1", Provider: "openrouter", APIKey: apiKey, Enabled: true,
		}); err != nil {
			t.Fatal(err)
		}

		s := f.open(Options{EncryptionKey: sampleFernetKey, AutoEncryptOnRead: false})
		if _, err := s.ListProjects(ctx); err != nil {
			t.Fatal(err)
		}

		stored := f.open(Options{})
		raw, _ := stored.ListProjects(ctx)
		if isEncrypted(raw[0].APIKey) {
			t.Error("关掉 AUTO_ENCRYPT_ON_READ 之后不该回写加密")
		}
	})
}

func TestNoKeyStoresPlaintext(t *testing.T) {
	eachBackend(t, "no-key", func(t *testing.T, f *fixture) {
		ctx := t.Context()
		s := f.open(Options{AutoEncryptOnRead: true})
		const apiKey = "sk-plaintext"
		if err := s.UpsertProject(ctx, model.Project{
			Name: "p1", Provider: "openrouter", APIKey: apiKey, Enabled: true,
		}); err != nil {
			t.Fatal(err)
		}
		got, err := s.ListProjects(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if got[0].APIKey != apiKey {
			t.Errorf("没设密钥时应当原样存取，得到 %q", got[0].APIKey)
		}
	})
}

// Implementation note.

// Implementation note.
// Implementation note.
func seedBalance(t *testing.T, s *sqlStore, at time.Time, projectID, name, provider string, balance float64, threshold *float64) {
	t.Helper()
	if err := s.q.insertBalance(t.Context(), insertBalanceParams{
		ProjectID:   projectID,
		ProjectName: name,
		Provider:    provider,
		Balance:     balance,
		Threshold:   nullFloatOf(threshold),
		BalanceType: sql.NullString{String: model.TypeCredits, Valid: true},
		NeedAlarm:   sql.NullBool{Bool: balance < 10, Valid: true},
		Timestamp:   nullTime(at),
	}); err != nil {
		t.Fatalf("写入余额记录失败: %v", err)
	}
}

func TestSaveAndQueryBalance(t *testing.T) {
	eachBackend(t, "balance", func(t *testing.T, f *fixture) {
		s := f.open(Options{})
		ctx := t.Context()

		threshold := 10.0
		if err := s.SaveBalance(ctx, BalanceRecord{
			ProjectID:   "pid-1",
			ProjectName: "生产账户",
			Provider:    "openrouter",
			Balance:     42.5,
			Threshold:   &threshold,
			BalanceType: model.TypeCredits,
			NeedAlarm:   false,
		}); err != nil {
			t.Fatal(err)
		}

		rows, err := s.BalanceHistory(ctx, BalanceQuery{})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 {
			t.Fatalf("期望 1 条余额记录，得到 %d 条", len(rows))
		}
		row := rows[0]
		if row.ProjectID != "pid-1" || row.ProjectName != "生产账户" || row.Balance != 42.5 {
			t.Errorf("读回的记录不对: %+v", row)
		}
		if row.Threshold == nil || *row.Threshold != threshold {
			t.Errorf("threshold = %v，期望 %v", row.Threshold, threshold)
		}
		if row.BalanceType != model.TypeCredits {
			t.Errorf("balance_type = %q，期望 %q", row.BalanceType, model.TypeCredits)
		}
		if row.ID == 0 {
			t.Error("自增主键没有回来")
		}
		// Implementation note.
		if _, err := time.Parse(time.RFC3339Nano, row.Timestamp); err != nil {
			t.Errorf("timestamp %q 不是合法 RFC3339: %v", row.Timestamp, err)
		}

		// Implementation note.
		if err := s.SaveBalance(ctx, BalanceRecord{
			ProjectID: "pid-2", ProjectName: "无阈值", Provider: "glm", Balance: 1,
		}); err != nil {
			t.Fatal(err)
		}
		rows, _ = s.BalanceHistory(ctx, BalanceQuery{ProjectID: "pid-2"})
		if len(rows) != 1 {
			t.Fatalf("期望 1 条，得到 %d 条", len(rows))
		}
		if rows[0].Threshold != nil {
			t.Errorf("没传阈值时应当是 nil，得到 %v", *rows[0].Threshold)
		}
		// Implementation note.
		if rows[0].BalanceType != model.TypeCredits {
			t.Errorf("balance_type = %q，期望补成 %q", rows[0].BalanceType, model.TypeCredits)
		}
	})
}

func TestBalanceHistoryFilters(t *testing.T) {
	eachBackend(t, "balance-filter", func(t *testing.T, f *fixture) {
		s := f.open(Options{})
		ctx := t.Context()
		now := time.Now().UTC()

		seedBalance(t, s, now.Add(-1*time.Hour), "pid-a", "A", "openrouter", 100, nil)
		seedBalance(t, s, now.Add(-2*time.Hour), "pid-b", "B", "glm", 90, nil)
		seedBalance(t, s, now.Add(-3*time.Hour), "pid-a", "A", "openrouter", 80, nil)
		seedBalance(t, s, now.AddDate(0, 0, -20), "pid-a", "A", "openrouter", 70, nil) // 窗口外

		all, err := s.BalanceHistory(ctx, BalanceQuery{Days: 7})
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 3 {
			t.Fatalf("7 天窗口内应当有 3 条，得到 %d 条", len(all))
		}
		// Implementation note.
		if all[0].Balance != 100 || all[2].Balance != 80 {
			t.Errorf("没有按时间倒序: %v %v %v", all[0].Balance, all[1].Balance, all[2].Balance)
		}

		byProject, err := s.BalanceHistory(ctx, BalanceQuery{Days: 7, ProjectID: "pid-a"})
		if err != nil {
			t.Fatal(err)
		}
		if len(byProject) != 2 {
			t.Errorf("按 project_id 过滤应当剩 2 条，得到 %d 条", len(byProject))
		}

		byProvider, err := s.BalanceHistory(ctx, BalanceQuery{Days: 7, Provider: "glm"})
		if err != nil {
			t.Fatal(err)
		}
		if len(byProvider) != 1 {
			t.Errorf("按 provider 过滤应当剩 1 条，得到 %d 条", len(byProvider))
		}

		both, err := s.BalanceHistory(ctx, BalanceQuery{Days: 7, ProjectID: "pid-a", Provider: "glm"})
		if err != nil {
			t.Fatal(err)
		}
		if len(both) != 0 {
			t.Errorf("两个过滤条件是与的关系，应当 0 条，得到 %d 条", len(both))
		}

		limited, err := s.BalanceHistory(ctx, BalanceQuery{Days: 7, Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		if len(limited) != 2 {
			t.Errorf("limit=2 应当只回 2 条，得到 %d 条", len(limited))
		}

		wide, err := s.BalanceHistory(ctx, BalanceQuery{Days: 30})
		if err != nil {
			t.Fatal(err)
		}
		if len(wide) != 4 {
			t.Errorf("30 天窗口应当有 4 条，得到 %d 条", len(wide))
		}
	})
}

func TestBalanceSeries(t *testing.T) {
	eachBackend(t, "balance-series", func(t *testing.T, f *fixture) {
		s := f.open(Options{})
		ctx := t.Context()
		now := time.Now().UTC()

		threshold := 5.0
		seedBalance(t, s, now.Add(-1*time.Hour), "pid-a", "A", "openrouter", 100, &threshold)
		seedBalance(t, s, now.Add(-3*time.Hour), "pid-b", "B", "glm", 50, nil)
		seedBalance(t, s, now.Add(-2*time.Hour), "pid-a", "A", "openrouter", 90, nil)
		seedBalance(t, s, now.AddDate(0, 0, -20), "pid-a", "A", "openrouter", 70, nil) // 窗口外

		series, err := s.BalanceSeries(ctx, 7)
		if err != nil {
			t.Fatal(err)
		}
		if len(series) != 3 {
			t.Fatalf("期望 3 个快照，得到 %d 个", len(series))
		}
		// Implementation note.
		if series[0].Balance != 50 || series[1].Balance != 90 || series[2].Balance != 100 {
			t.Errorf("没有按时间升序: %v", []float64{series[0].Balance, series[1].Balance, series[2].Balance})
		}
		for i := 1; i < len(series); i++ {
			if series[i].Timestamp < series[i-1].Timestamp {
				t.Errorf("第 %d 个点的时间戳比前一个早", i)
			}
		}
		last := series[2]
		if last.ProjectID != "pid-a" || last.ProjectName != "A" || last.Provider != "openrouter" {
			t.Errorf("快照字段不对: %+v", last)
		}
		if last.BalanceType != model.TypeCredits {
			t.Errorf("balance_type = %q", last.BalanceType)
		}
		if last.Threshold == nil || *last.Threshold != threshold {
			t.Errorf("threshold = %v，期望 %v", last.Threshold, threshold)
		}
		if series[1].Threshold != nil {
			t.Errorf("没有阈值的点应当是 nil，得到 %v", *series[1].Threshold)
		}
		// Implementation note.
		if delta := now.Unix() - last.Timestamp; delta < 3000 || delta > 4200 {
			t.Errorf("时间戳看起来不是 UTC Unix 秒，距今 %d 秒", delta)
		}
	})
}

func TestBalanceTrend(t *testing.T) {
	eachBackend(t, "trend", func(t *testing.T, f *fixture) {
		s := f.open(Options{})
		ctx := t.Context()
		now := time.Now().UTC()

		// Implementation note.
		trend, err := s.BalanceTrend(ctx, "pid-missing", 30)
		if err != nil {
			t.Fatal(err)
		}
		if trend != nil {
			t.Fatalf("没有数据时应当返回 nil，得到 %+v", trend)
		}

		threshold := 10.0
		seedBalance(t, s, now.Add(-72*time.Hour), "pid-a", "生产账户", "openrouter", 100, &threshold)
		seedBalance(t, s, now.Add(-48*time.Hour), "pid-a", "生产账户", "openrouter", 40, &threshold)
		seedBalance(t, s, now.Add(-24*time.Hour), "pid-a", "生产账户", "openrouter", 60, &threshold)
		seedBalance(t, s, now.AddDate(0, 0, -40), "pid-a", "生产账户", "openrouter", 999, &threshold) // 窗口外

		trend, err = s.BalanceTrend(ctx, "pid-a", 30)
		if err != nil {
			t.Fatal(err)
		}
		if trend == nil {
			t.Fatal("有数据时不该返回 nil")
		}
		if trend.ProjectID != "pid-a" || trend.ProjectName != "生产账户" {
			t.Errorf("项目信息不对: %+v", trend)
		}
		if trend.Days != 30 || trend.DataPoints != 3 {
			t.Errorf("days=%d data_points=%d，期望 30 / 3", trend.Days, trend.DataPoints)
		}
		if trend.CurrentBalance != 60 {
			t.Errorf("current_balance = %v，期望 60（最后一条）", trend.CurrentBalance)
		}
		if trend.MinBalance != 40 || trend.MaxBalance != 100 {
			t.Errorf("min=%v max=%v，期望 40 / 100", trend.MinBalance, trend.MaxBalance)
		}
		if want := (100.0 + 40 + 60) / 3; trend.AvgBalance != want {
			t.Errorf("avg_balance = %v，期望 %v", trend.AvgBalance, want)
		}
		if trend.Threshold != threshold {
			t.Errorf("threshold = %v，期望 %v", trend.Threshold, threshold)
		}
		if len(trend.History) != 3 {
			t.Fatalf("history 应当有 3 个点，得到 %d 个", len(trend.History))
		}
		// Implementation note.
		if trend.History[0].Balance != 100 || trend.History[2].Balance != 60 {
			t.Errorf("history 没有按时间升序: %+v", trend.History)
		}
		if trend.FirstTimestamp == "" || trend.LastTimestamp == "" {
			t.Error("首尾时间戳不该为空")
		}
		if trend.Change == nil || *trend.Change != -40 {
			t.Errorf("change = %v，期望 -40", trend.Change)
		}
		if trend.ChangePercent == nil || *trend.ChangePercent != -40 {
			t.Errorf("change_percent = %v，期望 -40", trend.ChangePercent)
		}
	})
}

func TestBalanceTrendSinglePointHasNoChange(t *testing.T) {
	eachBackend(t, "trend-single", func(t *testing.T, f *fixture) {
		s := f.open(Options{})
		ctx := t.Context()

		seedBalance(t, s, time.Now().UTC().Add(-time.Hour), "pid-a", "A", "openrouter", 12.5, nil)

		trend, err := s.BalanceTrend(ctx, "pid-a", 30)
		if err != nil {
			t.Fatal(err)
		}
		if trend == nil {
			t.Fatal("有一条数据也该出趋势")
		}
		if trend.DataPoints != 1 {
			t.Fatalf("data_points = %d，期望 1", trend.DataPoints)
		}
		// Implementation note.
		if trend.Change != nil || trend.ChangePercent != nil {
			t.Errorf("单点趋势不该有 change/change_percent: %v %v", trend.Change, trend.ChangePercent)
		}
		// Implementation note.
		if trend.Threshold != 0 {
			t.Errorf("threshold = %v，期望 0", trend.Threshold)
		}
	})
}

// Implementation note.
func TestBalanceTrendZeroStartDoesNotDivide(t *testing.T) {
	eachBackend(t, "trend-zero-start", func(t *testing.T, f *fixture) {
		s := f.open(Options{})
		ctx := t.Context()
		now := time.Now().UTC()

		seedBalance(t, s, now.Add(-48*time.Hour), "pid-zero", "刚充值", "openrouter", 0, nil)
		seedBalance(t, s, now.Add(-24*time.Hour), "pid-zero", "刚充值", "openrouter", 500, nil)

		trend, err := s.BalanceTrend(ctx, "pid-zero", 30)
		if err != nil {
			t.Fatal(err)
		}
		if trend.Change == nil || *trend.Change != 500 {
			t.Errorf("change = %v，期望 500", trend.Change)
		}
		if trend.ChangePercent == nil {
			t.Fatal("change_percent 不该是 nil")
		}
		if *trend.ChangePercent != 0 {
			t.Errorf("起点是 0 时 change_percent 应当记 0，得到 %v", *trend.ChangePercent)
		}
	})
}

// Implementation note.

func seedAlert(t *testing.T, s *sqlStore, at time.Time, projectID, name, alertType, status string) {
	t.Helper()
	if err := s.q.insertAlert(t.Context(), insertAlertParams{
		ProjectID:   projectID,
		ProjectName: name,
		AlertType:   alertType,
		Status:      sql.NullString{String: status, Valid: true},
		Message:     sql.NullString{String: "余额不足", Valid: true},
		Timestamp:   nullTime(at),
	}); err != nil {
		t.Fatalf("写入告警记录失败: %v", err)
	}
}

func TestSaveAndQueryAlerts(t *testing.T) {
	eachBackend(t, "alert", func(t *testing.T, f *fixture) {
		s := f.open(Options{})
		ctx := t.Context()

		value, threshold := 3.5, 10.0
		if err := s.SaveAlert(ctx, AlertRecord{
			AlertID:   "pid-1",
			Name:      "生产账户",
			AlertType: "low_balance",
			Message:   "余额 3.5 低于阈值 10",
			Value:     &value,
			Threshold: &threshold,
		}); err != nil {
			t.Fatal(err)
		}

		rows, err := s.RecentAlerts(ctx, AlertQuery{})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 {
			t.Fatalf("期望 1 条告警，得到 %d 条", len(rows))
		}
		row := rows[0]
		if row.ProjectID != "pid-1" || row.ProjectName != "生产账户" || row.AlertType != "low_balance" {
			t.Errorf("告警字段不对: %+v", row)
		}
		// Implementation note.
		if row.Status != "sent" {
			t.Errorf("status = %q，期望补成 sent", row.Status)
		}
		if row.BalanceValue == nil || *row.BalanceValue != value {
			t.Errorf("balance_value = %v，期望 %v", row.BalanceValue, value)
		}
		if row.ThresholdValue == nil || *row.ThresholdValue != threshold {
			t.Errorf("threshold_value = %v，期望 %v", row.ThresholdValue, threshold)
		}
		if row.Message != "余额 3.5 低于阈值 10" {
			t.Errorf("message = %q", row.Message)
		}
	})
}

func TestRecentAlertsRepairsLegacyRenewalMessages(t *testing.T) {
	eachBackend(t, "legacy-renewal", func(t *testing.T, f *fixture) {
		s := f.open(Options{})
		for _, message := range []string{
			"Subscription renewal reminder: Claude Max operation 2 operation",
			"Subscription renewal reminder: GitHub Copilot operation 0 operation — send failed: HTTP 500",
			"Subscription renewal reminder: Cursor renews in 3 days",
		} {
			if err := s.SaveAlert(t.Context(), AlertRecord{AlertID: "sub", Name: "sub", AlertType: "subscription_renewal", Message: message}); err != nil {
				t.Fatal(err)
			}
		}
		rows, err := s.RecentAlerts(t.Context(), AlertQuery{})
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]bool{}
		for _, row := range rows {
			got[row.Message] = true
		}
		for _, want := range []string{
			"Subscription renewal reminder: Claude Max renews in 2 days",
			"Subscription renewal reminder: GitHub Copilot renews today — send failed: HTTP 500",
			"Subscription renewal reminder: Cursor renews in 3 days",
		} {
			if !got[want] {
				t.Errorf("缺少修复后的文案 %q，实际 %v", want, got)
			}
		}
	})
}

func TestRecentAlertsFilters(t *testing.T) {
	eachBackend(t, "alert-filter", func(t *testing.T, f *fixture) {
		s := f.open(Options{})
		ctx := t.Context()
		now := time.Now().UTC()

		seedAlert(t, s, now.Add(-1*time.Hour), "pid-a", "A", "low_balance", "sent")
		seedAlert(t, s, now.Add(-2*time.Hour), "pid-a", "A", "low_runway", "sent")
		seedAlert(t, s, now.Add(-3*time.Hour), "pid-b", "B", "low_balance", "sent")
		seedAlert(t, s, now.AddDate(0, 0, -20), "pid-a", "A", "low_balance", "sent") // 窗口外

		all, err := s.RecentAlerts(ctx, AlertQuery{Days: 7})
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 3 {
			t.Fatalf("7 天内应当有 3 条，得到 %d 条", len(all))
		}
		if all[0].ProjectID != "pid-a" || all[0].AlertType != "low_balance" {
			t.Errorf("没有按时间倒序: %+v", all[0])
		}

		byProject, _ := s.RecentAlerts(ctx, AlertQuery{Days: 7, ProjectID: "pid-a"})
		if len(byProject) != 2 {
			t.Errorf("按项目过滤应当剩 2 条，得到 %d 条", len(byProject))
		}
		byType, _ := s.RecentAlerts(ctx, AlertQuery{Days: 7, AlertType: "low_balance"})
		if len(byType) != 2 {
			t.Errorf("按类型过滤应当剩 2 条，得到 %d 条", len(byType))
		}
		both, _ := s.RecentAlerts(ctx, AlertQuery{Days: 7, ProjectID: "pid-a", AlertType: "low_runway"})
		if len(both) != 1 {
			t.Errorf("两个条件是与的关系，应当剩 1 条，得到 %d 条", len(both))
		}
		limited, _ := s.RecentAlerts(ctx, AlertQuery{Days: 7, Limit: 1})
		if len(limited) != 1 {
			t.Errorf("limit=1 应当只回 1 条，得到 %d 条", len(limited))
		}
	})
}

func TestHasRecentAlert(t *testing.T) {
	eachBackend(t, "cooldown", func(t *testing.T, f *fixture) {
		s := f.open(Options{})
		ctx := t.Context()
		now := time.Now().UTC()

		seedAlert(t, s, now.Add(-30*time.Minute), "pid-a", "A", "low_balance", "sent")

		cases := []struct {
			name      string
			alertID   string
			alertType string
			within    time.Duration
			want      bool
		}{
			{"窗口内发过", "pid-a", "low_balance", time.Hour, true},
			{"窗口比间隔短", "pid-a", "low_balance", 10 * time.Minute, false},
			{"换一种告警类型", "pid-a", "low_runway", time.Hour, false},
			{"换一个项目", "pid-b", "low_balance", time.Hour, false},
			{"冷却时间为 0 一律放行", "pid-a", "low_balance", 0, false},
			{"冷却时间为负一律放行", "pid-a", "low_balance", -time.Hour, false},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				got, err := s.HasRecentAlert(ctx, tc.alertID, tc.alertType, tc.within)
				if err != nil {
					t.Fatal(err)
				}
				if got != tc.want {
					t.Errorf("HasRecentAlert = %v，期望 %v", got, tc.want)
				}
			})
		}
	})
}

// Implementation note.
func TestHasRecentAlertOnlyCountsSent(t *testing.T) {
	eachBackend(t, "cooldown-status", func(t *testing.T, f *fixture) {
		s := f.open(Options{})
		ctx := t.Context()
		now := time.Now().UTC()

		seedAlert(t, s, now.Add(-10*time.Minute), "pid-a", "A", "low_balance", "failed")
		seedAlert(t, s, now.Add(-10*time.Minute), "pid-b", "B", "low_balance", "pending")

		for _, id := range []string{"pid-a", "pid-b"} {
			got, err := s.HasRecentAlert(ctx, id, "low_balance", time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if got {
				t.Errorf("%s 只有未发成功的记录，不该进冷却", id)
			}
		}

		seedAlert(t, s, now.Add(-10*time.Minute), "pid-a", "A", "low_balance", "sent")
		got, err := s.HasRecentAlert(ctx, "pid-a", "low_balance", time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if !got {
			t.Error("有 sent 记录之后应当进冷却")
		}
	})
}

func TestAlertStats(t *testing.T) {
	eachBackend(t, "stats", func(t *testing.T, f *fixture) {
		s := f.open(Options{})
		ctx := t.Context()
		now := time.Now().UTC()

		empty, err := s.AlertStats(ctx, 30)
		if err != nil {
			t.Fatal(err)
		}
		if empty == nil {
			t.Fatal("空库也该出统计")
		}
		if empty.TotalAlerts != 0 || len(empty.ByType) != 0 || len(empty.TopProjects) != 0 {
			t.Errorf("空库统计应当全为空: %+v", empty)
		}
		// Implementation note.
		if empty.ByType == nil || empty.TopProjects == nil {
			t.Error("by_type 与 top_projects 不该是 nil")
		}

		seedAlert(t, s, now.Add(-1*time.Hour), "pid-a", "A", "low_balance", "sent")
		seedAlert(t, s, now.Add(-2*time.Hour), "pid-a", "A", "low_balance", "sent")
		seedAlert(t, s, now.Add(-3*time.Hour), "pid-a", "A", "low_runway", "failed")
		seedAlert(t, s, now.Add(-4*time.Hour), "pid-b", "B", "low_balance", "sent")
		seedAlert(t, s, now.AddDate(0, 0, -40), "pid-c", "C", "low_balance", "sent") // 窗口外

		stats, err := s.AlertStats(ctx, 30)
		if err != nil {
			t.Fatal(err)
		}
		if stats.Days != 30 {
			t.Errorf("days = %d，期望 30", stats.Days)
		}
		// Implementation note.
		if stats.TotalAlerts != 4 {
			t.Errorf("total_alerts = %d，期望 4", stats.TotalAlerts)
		}
		if stats.ByType["low_balance"] != 3 || stats.ByType["low_runway"] != 1 {
			t.Errorf("by_type = %v", stats.ByType)
		}
		if len(stats.TopProjects) != 2 {
			t.Fatalf("top_projects 应当有 2 项，得到 %d 项", len(stats.TopProjects))
		}
		// Implementation note.
		if stats.TopProjects[0].Project != "A" || stats.TopProjects[0].Count != 3 {
			t.Errorf("top_projects[0] = %+v，期望 A/3", stats.TopProjects[0])
		}
		if stats.TopProjects[1].Project != "B" || stats.TopProjects[1].Count != 1 {
			t.Errorf("top_projects[1] = %+v，期望 B/1", stats.TopProjects[1])
		}
	})
}

func TestAlertStatsTopProjectsCapped(t *testing.T) {
	eachBackend(t, "stats-top10", func(t *testing.T, f *fixture) {
		s := f.open(Options{})
		ctx := t.Context()
		now := time.Now().UTC()

		// Implementation note.
		for i := range 12 {
			name := string(rune('A' + i))
			for range i + 1 {
				seedAlert(t, s, now.Add(-time.Hour), "pid-"+name, name, "low_balance", "sent")
			}
		}

		stats, err := s.AlertStats(ctx, 30)
		if err != nil {
			t.Fatal(err)
		}
		if len(stats.TopProjects) != defaultTopProjects {
			t.Fatalf("top_projects 应当截到 %d 项，得到 %d 项", defaultTopProjects, len(stats.TopProjects))
		}
		if stats.TopProjects[0].Count != 12 {
			t.Errorf("排第一的应当是 12 次，得到 %d", stats.TopProjects[0].Count)
		}
		for i := 1; i < len(stats.TopProjects); i++ {
			if stats.TopProjects[i].Count > stats.TopProjects[i-1].Count {
				t.Errorf("top_projects 没有按计数倒序: %+v", stats.TopProjects)
			}
		}
	})
}

// Implementation note.

func TestSaveAndQueryEmailAlerts(t *testing.T) {
	eachBackend(t, "email", func(t *testing.T, f *fixture) {
		s := f.open(Options{})
		ctx := t.Context()

		amount := 128.5
		service := "Anthropic"
		if err := s.SaveEmailAlert(ctx, EmailAlertRecord{
			Mailbox:     "billing@example.com",
			Sender:      "no-reply@anthropic.com",
			Subject:     "您的账单已出账 <重要>",
			Date:        "Tue, 15 Sep 2026 10:00:00 +0800",
			ServiceName: &service,
			Amount:      &amount,
			Keywords:    []string{"账单", "扣费", "R&D"},
			AlertSent:   true,
		}); err != nil {
			t.Fatal(err)
		}

		rows, err := s.EmailAlerts(ctx, EmailAlertQuery{})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 {
			t.Fatalf("期望 1 条邮件记录，得到 %d 条", len(rows))
		}
		row := rows[0]
		if row.Mailbox != "billing@example.com" || row.Sender != "no-reply@anthropic.com" {
			t.Errorf("邮件字段不对: %+v", row)
		}
		if row.ServiceName == nil || *row.ServiceName != service {
			t.Errorf("service_name = %v", row.ServiceName)
		}
		if row.Amount == nil || *row.Amount != amount {
			t.Errorf("amount = %v", row.Amount)
		}
		if !row.AlertSent {
			t.Error("alert_sent 应当是 true")
		}
		// Stored as JSON array text (see TestKeywordStorageFormat), returned as a list.
		if want := []string{"账单", "扣费", "R&D"}; !reflect.DeepEqual(row.MatchedKeywords, want) {
			t.Errorf("matched_keywords = %q，期望 %q", row.MatchedKeywords, want)
		}

		// Implementation note.
		if err := s.SaveEmailAlert(ctx, EmailAlertRecord{
			Mailbox: "m2", Sender: "s", Subject: "无关键词", Date: "d",
		}); err != nil {
			t.Fatal(err)
		}
		rows, _ = s.EmailAlerts(ctx, EmailAlertQuery{Mailbox: "m2"})
		if len(rows) != 1 {
			t.Fatalf("期望 1 条，得到 %d 条", len(rows))
		}
		if rows[0].MatchedKeywords == nil || len(rows[0].MatchedKeywords) != 0 {
			t.Errorf("matched_keywords = %v，期望空列表（JSON 里是 [] 而不是 null）", rows[0].MatchedKeywords)
		}
		if rows[0].Amount != nil || rows[0].ServiceName != nil {
			t.Error("没提取到的字段应当是 nil")
		}
	})
}

func TestHasRecentEmailAlert(t *testing.T) {
	eachBackend(t, "email-dedup", func(t *testing.T, f *fixture) {
		s := f.open(Options{})
		ctx := t.Context()

		const (
			mailbox = "billing@example.com"
			sender  = "no-reply@anthropic.com"
			subject = "您的账单已出账"
			date    = "Tue, 15 Sep 2026 10:00:00 +0800"
		)
		if err := s.SaveEmailAlert(ctx, EmailAlertRecord{
			Mailbox: mailbox, Sender: sender, Subject: subject, Date: date, AlertSent: true,
		}); err != nil {
			t.Fatal(err)
		}

		cases := []struct {
			name                               string
			mailbox, sender, subject, dateText string
			want                               bool
		}{
			{"四个字段全同才算重复", mailbox, sender, subject, date, true},
			{"换邮箱", "other@example.com", sender, subject, date, false},
			{"换发件人", mailbox, "other@x.com", subject, date, false},
			{"换主题", mailbox, sender, "另一封", date, false},
			{"换日期", mailbox, sender, subject, "Wed, 16 Sep 2026 10:00:00 +0800", false},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				got, err := s.HasRecentEmailAlert(ctx, tc.mailbox, tc.sender, tc.subject, tc.dateText, 30)
				if err != nil {
					t.Fatal(err)
				}
				if got != tc.want {
					t.Errorf("HasRecentEmailAlert = %v，期望 %v", got, tc.want)
				}
			})
		}

		// Implementation note.
		if err := s.SaveEmailAlert(ctx, EmailAlertRecord{
			Mailbox: "m2", Sender: sender, Subject: subject, Date: date, AlertSent: false,
		}); err != nil {
			t.Fatal(err)
		}
		got, err := s.HasRecentEmailAlert(ctx, "m2", sender, subject, date, 30)
		if err != nil {
			t.Fatal(err)
		}
		if got {
			t.Error("alert_sent=false 的记录不该算作已发过")
		}
	})
}

func TestEmailAlertsFilters(t *testing.T) {
	eachBackend(t, "email-filter", func(t *testing.T, f *fixture) {
		s := f.open(Options{})
		ctx := t.Context()
		now := time.Now().UTC()

		seed := func(at time.Time, mailbox, subject string) {
			t.Helper()
			if err := s.q.insertEmailAlert(ctx, insertEmailAlertParams{
				Mailbox:         mailbox,
				Sender:          "s@x.com",
				Subject:         subject,
				Date:            "d",
				MatchedKeywords: sql.NullString{String: "[]", Valid: true},
				AlertSent:       sql.NullBool{Bool: true, Valid: true},
				Timestamp:       nullTime(at),
			}); err != nil {
				t.Fatalf("写入邮件记录失败: %v", err)
			}
		}
		seed(now.Add(-1*time.Hour), "a@x.com", "最新")
		seed(now.Add(-2*time.Hour), "b@x.com", "其次")
		seed(now.Add(-3*time.Hour), "a@x.com", "更早")
		seed(now.AddDate(0, 0, -40), "a@x.com", "窗口外")

		all, err := s.EmailAlerts(ctx, EmailAlertQuery{Days: 30})
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 3 {
			t.Fatalf("30 天内应当有 3 条，得到 %d 条", len(all))
		}
		if all[0].Subject != "最新" {
			t.Errorf("没有按时间倒序，第一条是 %q", all[0].Subject)
		}

		byMailbox, _ := s.EmailAlerts(ctx, EmailAlertQuery{Days: 30, Mailbox: "a@x.com"})
		if len(byMailbox) != 2 {
			t.Errorf("按邮箱过滤应当剩 2 条，得到 %d 条", len(byMailbox))
		}
		limited, _ := s.EmailAlerts(ctx, EmailAlertQuery{Days: 30, Limit: 1})
		if len(limited) != 1 {
			t.Errorf("limit=1 应当只回 1 条，得到 %d 条", len(limited))
		}
		wide, _ := s.EmailAlerts(ctx, EmailAlertQuery{Days: 60})
		if len(wide) != 4 {
			t.Errorf("60 天窗口应当有 4 条，得到 %d 条", len(wide))
		}
	})
}

// Implementation note.

func TestOpenCreatesSQLiteDirectory(t *testing.T) {
	// Implementation note.
	dir := filepath.Join(t.TempDir(), "nested", "data")
	s, err := Open(t.Context(), Options{DatabaseURL: "sqlite:///" + filepath.Join(dir, "app.db")})
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer s.Close()

	if _, err := os.Stat(filepath.Join(dir, "app.db")); err != nil {
		t.Errorf("库文件没有建出来: %v", err)
	}
	if !s.Enabled() {
		t.Error("落库版 Store 的 Enabled 应当是 true")
	}
}

// Implementation note.
func TestCreateTablesIsIdempotent(t *testing.T) {
	eachBackend(t, "create-tables", func(t *testing.T, f *fixture) {
		s := f.open(Options{})
		ctx := t.Context()
		if err := s.UpsertProject(ctx, model.Project{Name: "p1", Provider: "openrouter", Enabled: true}); err != nil {
			t.Fatal(err)
		}
		// Implementation note.
		again := f.open(Options{})
		got, err := again.ListProjects(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Errorf("重复建表之后数据应当还在，却有 %d 条", len(got))
		}
	})
}

func TestOpenRejectsBadURL(t *testing.T) {
	if _, err := Open(t.Context(), Options{DatabaseURL: "oracle://u:p@h/db"}); err == nil {
		t.Fatal("不认识的数据库类型应当报错")
	}
}

// Implementation note.

// Implementation note.
// Implementation note.
// Implementation note.
const legacyTimeLayout = "2006-01-02 15:04:05.000000"

// Implementation note.
// Implementation note.
func TestLegacyAndNewTimestampsCompareCorrectly(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "legacy.db")

	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// Implementation note.
	if _, err := legacy.ExecContext(ctx, `CREATE TABLE balance_history (
	id INTEGER NOT NULL, 
	project_id VARCHAR(200) NOT NULL, 
	project_name VARCHAR(200) NOT NULL, 
	provider VARCHAR(50) NOT NULL, 
	balance FLOAT NOT NULL, 
	threshold FLOAT, 
	balance_type VARCHAR(20), 
	need_alarm BOOLEAN, 
	timestamp DATETIME, 
	PRIMARY KEY (id)
)`); err != nil {
		t.Fatal(err)
	}
	written := time.Now().UTC().Add(-2 * time.Hour)
	if _, err := legacy.ExecContext(ctx,
		`INSERT INTO balance_history (project_id, project_name, provider, balance, threshold, balance_type, need_alarm, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"f4c50bfa74c0cf0c11cfb68aed887252", "glm", "glm", 98.7, 10.0, "quota", 0,
		written.Format(legacyTimeLayout)); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(ctx, Options{DatabaseURL: "sqlite:///" + path})
	if err != nil {
		t.Fatalf("打开现有生产库失败: %v", err)
	}
	defer s.Close()

	rows, err := s.BalanceHistory(ctx, BalanceQuery{Days: 7})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("应当读出旧格式的那 1 条，得到 %d 条", len(rows))
	}
	if rows[0].ProjectName != "glm" || rows[0].Balance != 98.7 || rows[0].BalanceType != "quota" {
		t.Errorf("读出来的字段不对: %+v", rows[0])
	}
	got, err := time.Parse(time.RFC3339Nano, rows[0].Timestamp)
	if err != nil {
		t.Fatalf("时间戳 %q 解析失败: %v", rows[0].Timestamp, err)
	}
	if diff := got.Sub(written); diff > time.Millisecond || diff < -time.Millisecond {
		t.Errorf("读回的时刻偏了 %v：%v vs %v", diff, got, written)
	}

	// Implementation note.
	if err := s.SaveBalance(ctx, BalanceRecord{
		ProjectID: "f4c50bfa74c0cf0c11cfb68aed887252", ProjectName: "glm", Provider: "glm", Balance: 97.2,
	}); err != nil {
		t.Fatal(err)
	}
	rows, err = s.BalanceHistory(ctx, BalanceQuery{Days: 7})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("新旧两条都该在 7 天窗口里，得到 %d 条", len(rows))
	}
	// Implementation note.
	if rows[0].Balance != 97.2 {
		t.Errorf("新旧行没有按时间正确排序: %+v", rows)
	}
}

// Implementation note.
// Implementation note.
// Implementation note.
func TestSQLiteTimestampStaysLegacyReadable(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "format.db")

	s, err := Open(ctx, Options{DatabaseURL: "sqlite:///" + path})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveBalance(ctx, BalanceRecord{
		ProjectID: "pid", ProjectName: "n", Provider: "p", Balance: 1,
	}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var stored string
	if err := raw.QueryRowContext(ctx, `SELECT CAST(timestamp AS TEXT) FROM balance_history LIMIT 1`).Scan(&stored); err != nil {
		t.Fatal(err)
	}

	// Implementation note.
	matched, err := regexp.MatchString(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}(\.\d+)?`, stored)
	if err != nil {
		t.Fatal(err)
	}
	if !matched {
		t.Errorf("落盘的时间戳 %q 不是历史行那种格式", stored)
	}
	if strings.Contains(stored, "UTC") {
		t.Errorf("落盘的时间戳 %q 用了驱动的默认格式，和历史行没法比较", stored)
	}
}

// Keywords are stored as JSON array text with CJK and & left unescaped, the way rows
// already in existing databases were written. Reading decodes them into a list.
func TestKeywordStorageFormat(t *testing.T) {
	text, err := encodeKeywords([]string{"账单", "扣费", "R&D"})
	if err != nil || text != `["账单","扣费","R&D"]` {
		t.Fatalf("存储格式变了: %q, %v", text, err)
	}
	if empty, _ := encodeKeywords(nil); empty != "[]" {
		t.Errorf("没有关键词时应存 []，实际 %q", empty)
	}
	cases := map[string][]string{
		`["账单","R&D"]`: {"账单", "R&D"},
		`[]`:           {},
		`null`:         {},
		`low balance`:  {"low balance"}, // not JSON: keep the text rather than drop it
	}
	for stored, want := range cases {
		if got := decodeKeywords(sql.NullString{String: stored, Valid: true}); !reflect.DeepEqual(got, want) {
			t.Errorf("decodeKeywords(%q) = %q，期望 %q", stored, got, want)
		}
	}
	if got := decodeKeywords(sql.NullString{}); got == nil || len(got) != 0 {
		t.Errorf("空列应解码成空列表: %v", got)
	}
}
