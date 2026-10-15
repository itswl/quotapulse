package config

import (
	"testing"

	"github.com/itswl/quotapulse/internal/model"
	_ "github.com/itswl/quotapulse/internal/provider" // 注册表要有内容，发现逻辑才有平台可枚举
)

func TestDiscoverProjects(t *testing.T) {
	t.Run("没有密钥就什么都不发现", func(t *testing.T) {
		if got := DiscoverProjects(nil); len(got) != 0 {
			t.Errorf("期望空，实际 %v", got)
		}
	})

	t.Run("设了密钥就自动成为受监控项目", func(t *testing.T) {
		t.Setenv("DEEPSEEK_API_KEY", "sk-test")
		got := DiscoverProjects(nil)
		if len(got) != 1 {
			t.Fatalf("期望 1 个项目，实际 %d 个", len(got))
		}
		p := got[0]
		if p.Name != "deepseek" || p.Provider != "deepseek" || p.APIKey != "sk-test" {
			t.Errorf("项目内容不对: %+v", p)
		}
		if !p.FromEnv {
			t.Error("自动发现的项目要标记 from_env，页面上才知道它是只读的")
		}
		if p.Threshold != 0 {
			t.Errorf("没设阈值时应为 0（永不告警），实际 %v", p.Threshold)
		}
		if p.Type != model.TypeBalance {
			t.Errorf("deepseek 的类型应是 balance，实际 %s", p.Type)
		}
	})

	t.Run("阈值与分组标签", func(t *testing.T) {
		t.Setenv("DEEPSEEK_API_KEY", "sk-test")
		t.Setenv("DEEPSEEK_THRESHOLD", "50")
		t.Setenv("DEEPSEEK_OWNER_PROJECT", "AI 平台")
		p := DiscoverProjects(nil)[0]
		if p.Threshold != 50 {
			t.Errorf("阈值应为 50，实际 %v", p.Threshold)
		}
		if p.OwnerProject == nil || *p.OwnerProject != "AI 平台" {
			t.Errorf("分组标签不对: %v", p.OwnerProject)
		}
	})

	t.Run("GLM 的类型是配额百分比", func(t *testing.T) {
		t.Setenv("GLM_API_KEY", "id.secret")
		if p := DiscoverProjects(nil)[0]; p.Type != model.TypeQuota {
			t.Errorf("GLM 应是 quota 类型，实际 %s", p.Type)
		}
	})

	t.Run("同一平台多个账号带序号", func(t *testing.T) {
		t.Setenv("VOLC_1_API_KEY", "ak1:sk1")
		t.Setenv("VOLC_2_API_KEY", "ak2:sk2")
		t.Setenv("VOLC_1_THRESHOLD", "7000")
		got := DiscoverProjects(nil)
		if len(got) != 2 {
			t.Fatalf("期望 2 个账号，实际 %d 个", len(got))
		}
		if got[0].Name != "volc-1" || got[1].Name != "volc-2" {
			t.Errorf("多账号应带序号后缀，实际 %s / %s", got[0].Name, got[1].Name)
		}
		if got[0].Threshold != 7000 || got[1].Threshold != 0 {
			t.Errorf("阈值应各认各的序号，实际 %v / %v", got[0].Threshold, got[1].Threshold)
		}
	})

	t.Run("单账号不带序号后缀", func(t *testing.T) {
		t.Setenv("VOLC_1_API_KEY", "ak:sk")
		if p := DiscoverProjects(nil)[0]; p.Name != "volc" {
			t.Errorf("只有一个账号时不该带后缀，实际 %s", p.Name)
		}
	})

	t.Run("不带序号与序号1是同一个账号", func(t *testing.T) {
		t.Setenv("VOLC_API_KEY", "ak:sk")
		t.Setenv("VOLC_1_API_KEY", "ak:sk")
		if got := DiscoverProjects(nil); len(got) != 1 {
			t.Errorf("同一账号被认成了 %d 个", len(got))
		}
	})

	t.Run("已声明的平台不重复添加", func(t *testing.T) {
		t.Setenv("DEEPSEEK_API_KEY", "sk-env")
		declared := []model.Project{{Name: "我的 DeepSeek", Provider: "deepseek", APIKey: "sk-db"}}
		if got := DiscoverProjects(declared); len(got) != 0 {
			t.Errorf("数据库里声明过的平台不该再自动添加，实际 %v", got)
		}
	})
}

func TestDiscoverMailboxes(t *testing.T) {
	t.Run("三个变量齐全才算数", func(t *testing.T) {
		t.Setenv("EMAIL_HOST", "imap.x.com")
		t.Setenv("EMAIL_USERNAME", "u@x.com")
		if got := DiscoverMailboxes(nil); len(got) != 0 {
			t.Error("少了密码不该纳入扫描")
		}
	})

	t.Run("默认端口与 SSL", func(t *testing.T) {
		t.Setenv("EMAIL_HOST", "imap.x.com")
		t.Setenv("EMAIL_USERNAME", "u@x.com")
		t.Setenv("EMAIL_PASSWORD", "pw")
		got := DiscoverMailboxes(nil)
		if len(got) != 1 {
			t.Fatalf("期望 1 个邮箱，实际 %d 个", len(got))
		}
		m := got[0]
		if m.Port != 993 || !m.UseSSL {
			t.Errorf("默认应是 993 + SSL，实际 %d / %v", m.Port, m.UseSSL)
		}
		if m.Name != "u@x.com" {
			t.Errorf("没写名称时应取账号，实际 %s", m.Name)
		}
		if !m.FromEnv {
			t.Error("自动发现的邮箱要标记 from_env")
		}
	})

	t.Run("覆盖端口名称与SSL", func(t *testing.T) {
		for k, v := range map[string]string{
			"EMAIL_HOST": "imap.x.com", "EMAIL_USERNAME": "u@x.com", "EMAIL_PASSWORD": "pw",
			"EMAIL_PORT": "143", "EMAIL_USE_SSL": "false", "EMAIL_NAME": "工作邮箱",
		} {
			t.Setenv(k, v)
		}
		m := DiscoverMailboxes(nil)[0]
		if m.Port != 143 || m.UseSSL || m.Name != "工作邮箱" {
			t.Errorf("覆盖没生效: %+v", m)
		}
	})

	t.Run("多个邮箱", func(t *testing.T) {
		for k, v := range map[string]string{
			"EMAIL_1_HOST": "a.com", "EMAIL_1_USERNAME": "a@a.com", "EMAIL_1_PASSWORD": "p1",
			"EMAIL_2_HOST": "b.com", "EMAIL_2_USERNAME": "b@b.com", "EMAIL_2_PASSWORD": "p2",
			"EMAIL_2_NAME": "备用",
		} {
			t.Setenv(k, v)
		}
		got := DiscoverMailboxes(nil)
		if len(got) != 2 || got[0].Name != "a@a.com" || got[1].Name != "备用" {
			t.Errorf("多邮箱发现有误: %+v", got)
		}
	})

	t.Run("名称或账号已声明过就不重复", func(t *testing.T) {
		t.Setenv("EMAIL_HOST", "a.com")
		t.Setenv("EMAIL_USERNAME", "a@a.com")
		t.Setenv("EMAIL_PASSWORD", "p")

		if got := DiscoverMailboxes([]model.Mailbox{{Name: "a@a.com"}}); len(got) != 0 {
			t.Error("同名邮箱不该重复添加")
		}
		// mailboxes renamed in the database must still be recognized by account, or the same inbox gets scanned twice
		if got := DiscoverMailboxes([]model.Mailbox{{Name: "别名", Username: "a@a.com"}}); len(got) != 0 {
			t.Error("同账号不同显示名的邮箱不该重复添加")
		}
	})
}

func TestProviderKeyEnvNames(t *testing.T) {
	tests := []struct {
		provider string
		ordinal  int
		want     []string
	}{
		{"deepseek", 1, []string{"DEEPSEEK_API_KEY", "DEEPSEEK_1_API_KEY"}},
		{"volc", 2, []string{"VOLC_2_API_KEY"}},
		{"", 1, nil},
	}
	for _, tt := range tests {
		got := ProviderKeyEnvNames(tt.provider, tt.ordinal)
		if len(got) != len(tt.want) {
			t.Errorf("ProviderKeyEnvNames(%q, %d) = %v，期望 %v", tt.provider, tt.ordinal, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("ProviderKeyEnvNames(%q, %d) = %v，期望 %v", tt.provider, tt.ordinal, got, tt.want)
				break
			}
		}
	}
}
