package store

import (
	"net/url"
	"strings"
	"testing"
)

func TestParseURL(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		engine   Engine
		driver   string
		dsn      string
		filePath string
	}{
		{
			name:     "sqlite 三斜杠是相对路径",
			input:    "sqlite:///./data/quotapulse.db",
			engine:   EngineSQLite,
			driver:   "sqlite",
			dsn:      "./data/quotapulse.db?_pragma=busy_timeout%285000%29&_time_format=sqlite",
			filePath: "./data/quotapulse.db",
		},
		{
			name:     "sqlite 四斜杠是绝对路径",
			input:    "sqlite:////var/lib/balance/app.db",
			engine:   EngineSQLite,
			driver:   "sqlite",
			dsn:      "/var/lib/balance/app.db?_pragma=busy_timeout%285000%29&_time_format=sqlite",
			filePath: "/var/lib/balance/app.db",
		},
		{
			name:     "sqlite 裸文件名",
			input:    "sqlite:///app.db",
			engine:   EngineSQLite,
			driver:   "sqlite",
			dsn:      "app.db?_pragma=busy_timeout%285000%29&_time_format=sqlite",
			filePath: "app.db",
		},
		{
			name:     "sqlite 内存库没有文件路径",
			input:    "sqlite://",
			engine:   EngineSQLite,
			driver:   "sqlite",
			dsn:      ":memory:?_pragma=busy_timeout%285000%29&_time_format=sqlite",
			filePath: "",
		},
		{
			name:     "sqlite 显式内存库",
			input:    "sqlite:///:memory:",
			engine:   EngineSQLite,
			driver:   "sqlite",
			dsn:      ":memory:?_pragma=busy_timeout%285000%29&_time_format=sqlite",
			filePath: "",
		},
		{
			name:     "sqlite 带驱动后缀",
			input:    "sqlite+pysqlite:///./data/x.db",
			engine:   EngineSQLite,
			driver:   "sqlite",
			dsn:      "./data/x.db?_pragma=busy_timeout%285000%29&_time_format=sqlite",
			filePath: "./data/x.db",
		},
		{
			name:   "sqlite 已有的时间格式参数不被覆盖",
			input:  "sqlite:///./x.db?_time_format=datetime&_pragma=foreign_keys(1)",
			engine: EngineSQLite,
			driver: "sqlite",
			dsn:    "./x.db?_pragma=foreign_keys%281%29&_time_format=datetime",

			filePath: "./x.db",
		},
		{
			name:   "postgresql 原样交给 pgx",
			input:  "postgresql://user:pass@db.internal:5432/quotapulse",
			engine: EnginePostgres,
			driver: "pgx",
			dsn:    "postgres://user:pass@db.internal:5432/quotapulse",
		},
		{
			name:   "postgres 短写法",
			input:  "postgres://user:pass@localhost:5432/quotapulse?sslmode=disable",
			engine: EnginePostgres,
			driver: "pgx",
			dsn:    "postgres://user:pass@localhost:5432/quotapulse?sslmode=disable",
		},
		{
			name:   "postgresql 去掉驱动后缀",
			input:  "postgresql+psycopg2://u:p@h:5432/db",
			engine: EnginePostgres,
			driver: "pgx",
			dsn:    "postgres://u:p@h:5432/db",
		},
		{
			name:   "mysql 带驱动后缀，保留 charset 并补 parseTime",
			input:  "mysql+pymysql://user:pass@db.internal:3306/quotapulse?charset=utf8mb4",
			engine: EngineMySQL,
			driver: "mysql",
			dsn:    "user:pass@tcp(db.internal:3306)/quotapulse?charset=utf8mb4&loc=UTC&parseTime=true",
		},
		{
			name:   "mysql 省略端口时补 3306",
			input:  "mysql://user:pass@db.internal/quotapulse",
			engine: EngineMySQL,
			driver: "mysql",
			dsn:    "user:pass@tcp(db.internal:3306)/quotapulse?loc=UTC&parseTime=true",
		},
		{
			name:   "mysql 百分号编码的密码要还原",
			input:  "mysql+pymysql://user:p%40ss%3Aword@h:3306/db",
			engine: EngineMySQL,
			driver: "mysql",
			dsn:    "user:p@ss:word@tcp(h:3306)/db?loc=UTC&parseTime=true",
		},
		{
			name:   "mysql 无密码",
			input:  "mysql://root@127.0.0.1:3307/db",
			engine: EngineMySQL,
			driver: "mysql",
			dsn:    "root@tcp(127.0.0.1:3307)/db?loc=UTC&parseTime=true",
		},
		{
			name:   "mariadb 按 mysql 处理",
			input:  "mariadb+pymysql://u:p@h:3306/db",
			engine: EngineMySQL,
			driver: "mysql",
			dsn:    "u:p@tcp(h:3306)/db?loc=UTC&parseTime=true",
		},
		{
			name:   "调用方自己写了 parseTime 就不再追加",
			input:  "mysql+pymysql://u:p@h:3306/db?parseTime=false&loc=Local",
			engine: EngineMySQL,
			driver: "mysql",
			dsn:    "u:p@tcp(h:3306)/db?loc=Local&parseTime=false",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseURL(tc.input)
			if err != nil {
				t.Fatalf("ParseURL(%q) 报错: %v", tc.input, err)
			}
			if got.Engine != tc.engine {
				t.Errorf("Engine = %q, 期望 %q", got.Engine, tc.engine)
			}
			if got.DriverName != tc.driver {
				t.Errorf("DriverName = %q, 期望 %q", got.DriverName, tc.driver)
			}
			if got.DSN != tc.dsn {
				t.Errorf("DSN = %q, 期望 %q", got.DSN, tc.dsn)
			}
			if got.FilePath != tc.filePath {
				t.Errorf("FilePath = %q, 期望 %q", got.FilePath, tc.filePath)
			}
		})
	}
}

func TestParseURLRejects(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"空串", ""},
		{"只有空白", "   "},
		{"缺少 scheme 分隔符", "/data/app.db"},
		{"不认识的引擎", "oracle://u:p@h:1521/db"},
		{"mongodb 不是关系库", "mongodb://localhost:27017/db"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseURL(tc.input); err == nil {
				t.Fatalf("ParseURL(%q) 应当报错", tc.input)
			}
		})
	}
}

// the SQLite driver parses the DSN as a query string, so parameters must be
// valid percent-encoding — otherwise the parentheses in busy_timeout(5000)
// keep the pragma from being read.
func TestSQLiteDSNQueryIsParsable(t *testing.T) {
	target, err := ParseURL("sqlite:///./data/x.db")
	if err != nil {
		t.Fatal(err)
	}
	_, query, _ := strings.Cut(target.DSN, "?")
	values, err := url.ParseQuery(query)
	if err != nil {
		t.Fatalf("DSN 查询串解析失败: %v", err)
	}
	if got := values.Get("_pragma"); got != "busy_timeout(5000)" {
		t.Errorf("_pragma = %q, 期望 busy_timeout(5000)", got)
	}
	if got := values.Get("_time_format"); got != "sqlite" {
		t.Errorf("_time_format = %q, 期望 sqlite", got)
	}
}

// TestPasswordWithURLSpecialCharacters: # and ? in a password must not
// truncate the connection string.
//
// A real production trap: an OCI-generated MySQL password contained #,
// net/url read it as a fragment start, the string broke there, reported
// "invalid port", and the service could not reach its database. The string
// is therefore split by our own regex, and these cases pin that behavior
// against anyone "simplifying" back to net/url.
func TestPasswordWithURLSpecialCharacters(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		wantDSN  string
		wantPass string // 仅用于错误信息
	}{
		{
			name:    "mysql 密码里有井号",
			url:     "mysql+pymysql://admin:pa#ss!^word@10.0.10.35:3306/quotapulse?charset=utf8mb4",
			wantDSN: "admin:pa#ss!^word@tcp(10.0.10.35:3306)/quotapulse?charset=utf8mb4&loc=UTC&parseTime=true",
		},
		{
			name:    "mysql 密码里有问号",
			url:     "mysql://admin:pa?ss@127.0.0.1:3306/db",
			wantDSN: "admin:pa?ss@tcp(127.0.0.1:3306)/db?loc=UTC&parseTime=true",
		},
		{
			name:    "mysql 密码里有斜杠",
			url:     "mysql://admin:pa/ss@127.0.0.1:3306/db",
			wantDSN: "admin:pa/ss@tcp(127.0.0.1:3306)/db?loc=UTC&parseTime=true",
		},
		{
			name:    "mysql 密码里有单独的百分号，不能当成坏转义报错",
			url:     "mysql://admin:100%pure@127.0.0.1:3306/db",
			wantDSN: "admin:100%pure@tcp(127.0.0.1:3306)/db?loc=UTC&parseTime=true",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseURL(tt.url)
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if got.DSN != tt.wantDSN {
				t.Errorf("DSN 不对\n  期望: %s\n  实际: %s", tt.wantDSN, got.DSN)
			}
		})
	}
}

// TestPostgresPasswordIsReEncoded: the postgres DSN is re-escaped before
// reaching pgx, because pgx internally uses net/url too and passing it through
// raw would truncate at # just the same.
func TestPostgresPasswordIsReEncoded(t *testing.T) {
	got, err := ParseURL("postgresql://admin:pa#ss@db.internal:5432/quotapulse?sslmode=require")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}

	// parse it back once more to confirm pgx receives a string that restores the original password
	parsed, err := url.Parse(got.DSN)
	if err != nil {
		t.Fatalf("生成的 DSN 自己都解析不了: %v（%s）", err, got.DSN)
	}
	password, _ := parsed.User.Password()
	if password != "pa#ss" {
		t.Errorf("密码还原错了，期望 pa#ss，实际 %q（DSN: %s）", password, got.DSN)
	}
	if parsed.Host != "db.internal:5432" {
		t.Errorf("主机不对: %q", parsed.Host)
	}
	if parsed.RawQuery != "sslmode=require" {
		t.Errorf("查询参数丢了: %q", parsed.RawQuery)
	}
}

// TestIPv6Host: IPv6 addresses must be recognized and brackets kept out of the hostname.
func TestIPv6Host(t *testing.T) {
	got, err := ParseURL("mysql://u:p@[2001:db8::1]:3306/db")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if !strings.Contains(got.DSN, "tcp([2001:db8::1]:3306)") {
		t.Errorf("IPv6 主机拼错了: %s", got.DSN)
	}
}
