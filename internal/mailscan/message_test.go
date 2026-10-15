package mailscan

import (
	"strings"
	"testing"
)

// rawMessage assembles an RFC822 message with protocol CRLF line endings.
func rawMessage(headers []string, body string) []byte {
	return []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + body)
}

func TestParseMessageDecodesMIMEHeaders(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		want    string
	}{
		// =?utf-8?B?...?= is the most common Chinese subject encoding; failing to decode it sends mojibake into keyword matching
		{"utf-8 base64", "=?utf-8?B?5L2Z6aKd5ZGK6K2m6YCa55+l?=", "余额告警通知"},
		{"gbk base64", "=?gbk?B?suLK1LHqzOI=?=", "测试标题"},
		{"quoted-printable", "=?utf-8?Q?=E6=AC=A0=E8=B4=B9?=", "欠费"},
		{"纯 ASCII 不动它", "Re: Payment Due", "Re: Payment Due"},
		{"编码词与明文混排", "=?utf-8?B?5L2Z6aKd5ZGK6K2m?= notice", "余额告警 notice"},
		{"认不出的字符集退回原样", "=?x-unknown-9?B?zzzz?=", "=?x-unknown-9?B?zzzz?="},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := rawMessage([]string{
				"Subject: " + tt.subject,
				"From: noreply@aliyun.com",
				"Date: Mon, 01 Sep 2026 10:00:00 +0800",
				"Content-Type: text/plain; charset=utf-8",
			}, "正文")

			msg, err := parseMessage(raw)
			if err != nil {
				t.Fatalf("parseMessage 报错: %v", err)
			}
			if msg.Subject != tt.want {
				t.Errorf("主题 = %q, 期望 %q", msg.Subject, tt.want)
			}
		})
	}
}

func TestParseMessageDecodesBody(t *testing.T) {
	tests := []struct {
		name     string
		headers  []string
		body     string
		contains string
	}{
		{
			name: "quoted-printable + utf-8",
			headers: []string{
				"Content-Type: text/plain; charset=utf-8",
				"Content-Transfer-Encoding: quoted-printable",
			},
			body:     "=E4=BD=99=E9=A2=9D=EF=BC=9A12.50=E5=85=83",
			contains: "余额：12.50元",
		},
		{
			name: "base64 + utf-8",
			headers: []string{
				"Content-Type: text/plain; charset=utf-8",
				"Content-Transfer-Encoding: base64",
			},
			body:     "5oKo55qE6LSm5oi35L2Z6aKd5LiN6Laz77yM6K+35Y+K5pe25YWF5YC844CC5L2Z6aKd77yaMTIuNTDlhYM=",
			contains: "余额不足",
		},
		{
			name: "gbk 正文",
			headers: []string{
				"Content-Type: text/plain; charset=gbk",
				"Content-Transfer-Encoding: base64",
			},
			body:     "xPq1xNXLu6fT4LbusrvX46Osx+u8sMqxs+TWtaGj0+C27qO6ODguODjUqg==",
			contains: "余额不足",
		},
		{
			name:     "没有 Content-Type 时按纯文本处理",
			headers:  nil,
			body:     "your balance is low",
			contains: "balance is low",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := rawMessage(append([]string{"Subject: 通知"}, tt.headers...), tt.body)
			msg, err := parseMessage(raw)
			if err != nil {
				t.Fatalf("parseMessage 报错: %v", err)
			}
			if !strings.Contains(msg.Body, tt.contains) {
				t.Errorf("正文 = %q, 期望包含 %q", msg.Body, tt.contains)
			}
		})
	}
}

func TestParseMessageMultipart(t *testing.T) {
	raw := []byte(strings.Join([]string{
		"Subject: =?utf-8?B?44CQ6Zi/6YeM5LqR44CR5L2Z6aKd5LiN6Laz5o+Q6YaS?=",
		"From: noreply@aliyun.com",
		"MIME-Version: 1.0",
		`Content-Type: multipart/mixed; boundary="sep"`,
		"",
		"--sep",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"纯文本部分",
		"--sep",
		"Content-Type: text/html; charset=utf-8",
		"",
		"<p>余额<b>不足</b></p>",
		"--sep",
		"Content-Type: text/plain; charset=utf-8",
		`Content-Disposition: attachment; filename="bill.txt"`,
		"",
		"附件内容",
		"--sep--",
		"",
	}, "\r\n"))

	msg, err := parseMessage(raw)
	if err != nil {
		t.Fatalf("parseMessage 报错: %v", err)
	}
	if msg.Subject != "【阿里云】余额不足提醒" {
		t.Errorf("主题 = %q", msg.Subject)
	}
	if !strings.Contains(msg.Body, "纯文本部分") {
		t.Errorf("正文 = %q, 期望包含纯文本分段", msg.Body)
	}
	if !strings.Contains(msg.Body, "余额") || strings.Contains(msg.Body, "<b>") {
		t.Errorf("正文 = %q, 期望 HTML 分段去掉标签后保留文字", msg.Body)
	}
	// attachment text entering the match range would false-positive on attachment words, so parsing skips it entirely
	if strings.Contains(msg.Body, "附件内容") {
		t.Errorf("正文 = %q, 附件不该进正文", msg.Body)
	}
}

func TestParseMessageID(t *testing.T) {
	headers := []string{
		"Subject: 余额不足提醒",
		"From: noreply@aliyun.com",
		"Date: Mon, 01 Sep 2026 10:00:00 +0800",
	}

	withID, err := parseMessage(rawMessage(append(headers, "Message-ID: <alert-1@aliyun.com>"), "正文"))
	if err != nil {
		t.Fatalf("parseMessage 报错: %v", err)
	}
	if withID.ID != "<alert-1@aliyun.com>" {
		t.Errorf("ID = %q, 期望直接用 Message-ID", withID.ID)
	}

	// without Message-ID fall back to md5(date|subject|from).
	// the expected value is computed independently; changing the implementation
	// shows up here
	fallback, err := parseMessage(rawMessage(headers, "正文"))
	if err != nil {
		t.Fatalf("parseMessage 报错: %v", err)
	}
	if want := "8ba451a1d2c341315eac472b9d5d38e6"; fallback.ID != want {
		t.Errorf("ID = %q, 期望 %q", fallback.ID, want)
	}

	other, err := parseMessage(rawMessage([]string{
		"Subject: 另一封", "From: noreply@aliyun.com", "Date: Mon, 01 Sep 2026 10:00:00 +0800",
	}, "正文"))
	if err != nil {
		t.Fatalf("parseMessage 报错: %v", err)
	}
	if other.ID == fallback.ID {
		t.Error("不同主题的邮件不该算成同一封")
	}
}

// Mails with broken headers must not be dropped: scan the raw message, keywords still match.
func TestParseMessageTolerantOfJunk(t *testing.T) {
	msg, err := parseMessage([]byte("这不是一封邮件，但里面写了欠费"))
	if err == nil {
		t.Error("格式有问题的邮件应当把错误带出来，供上层记日志")
	}
	if !strings.Contains(msg.Body, "欠费") {
		t.Errorf("正文 = %q, 期望保留原文", msg.Body)
	}
	if msg.ID == "" {
		t.Error("去重键不该为空，否则一次扫描里所有坏邮件会互相顶掉")
	}
	if other, _ := parseMessage([]byte("另一封坏邮件")); other.ID == msg.ID {
		t.Error("两封不同的坏邮件不该算成同一封")
	}
}
