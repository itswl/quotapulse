package store

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

// Engine is the detected database kind, deciding which generated code and DDL to use.
type Engine string

const (
	EngineSQLite   Engine = "sqlite"
	EnginePostgres Engine = "postgres"
	EngineMySQL    Engine = "mysql"
)

// Target is what a DATABASE_URL translates into.
//
// The config carries a URL-style connection string
// (scheme://user:password@host:port/database?params); this renders the DSN
// each Go driver understands.
type Target struct {
	Engine     Engine
	DriverName string // database/sql registration name
	DSN        string
	FilePath   string // SQLite only: database file path; Open creates the parent directory first
}

// ParseURL translates a URL-style DATABASE_URL into the Go driver's DSN.
func ParseURL(databaseURL string) (Target, error) {
	raw := strings.TrimSpace(databaseURL)
	if raw == "" {
		return Target{}, fmt.Errorf("DATABASE_URL is empty")
	}

	scheme, rest, ok := strings.Cut(raw, "://")
	if !ok {
		return Target{}, fmt.Errorf("DATABASE_URL is missing \"://\": %q", databaseURL)
	}
	// the scheme may be written dialect+driver (mysql+pymysql,
	// postgresql+psycopg2): the part after + is a driver name; we take only the
	// database half and pick the driver ourselves
	dialect, _, _ := strings.Cut(strings.ToLower(scheme), "+")

	switch dialect {
	case "sqlite":
		return sqliteTarget(rest)
	case "postgresql", "postgres":
		return postgresTarget(rest)
	case "mysql", "mariadb":
		return mysqlTarget(rest)
	default:
		return Target{}, fmt.Errorf("Unsupported database type %q; only sqlite / postgresql / mysql are supported", dialect)
	}
}

// urlParts is a connection string taken apart: scheme://user:password@host:port/database?params.
type urlParts struct {
	User     string
	Password string
	HasPass  bool
	Host     string
	Port     string
	Database string
	Query    string
}

// parseConnURL splits the connection string with its own regex instead of
// net/url.
//
// The difference is fatal: net/url treats # as a fragment start and ? as a
// query start, and both are common in database passwords. Production hit
// this — a single # in a password truncated the string there and surfaced as
// "invalid port". This regex treats only the first @ as the credentials
// boundary; # and ? inside the password are ordinary characters.
var connURLPattern = regexp.MustCompile(
	`^(?:(?P<user>[^:/@]*)(?::(?P<pass>[^@]*))?@)?` + // credentials; password runs to the first @
		`(?:\[(?P<v6>[^\]]+)\]|(?P<host>[^/:@]*))?` + // host, [::1] form supported
		`(?::(?P<port>[0-9]*))?` +
		`(?:/(?P<db>[^?]*))?` +
		`(?:\?(?P<query>.*))?$`)

func parseConnURL(rest string) (urlParts, error) {
	m := connURLPattern.FindStringSubmatch(rest)
	if m == nil {
		return urlParts{}, fmt.Errorf("Unrecognized connection string")
	}
	group := func(name string) string {
		return m[connURLPattern.SubexpIndex(name)]
	}

	host := group("host")
	if v6 := group("v6"); v6 != "" {
		host = v6
	}
	parts := urlParts{
		User:     unescapeLenient(group("user")),
		Host:     host,
		Port:     group("port"),
		Database: group("db"),
		Query:    group("query"),
	}
	// distinguish absent from present-but-empty passwords: they are different things
	if idx := strings.Index(rest, "@"); idx >= 0 {
		credentials := rest[:idx]
		if _, pass, ok := strings.Cut(credentials, ":"); ok {
			parts.Password, parts.HasPass = unescapeLenient(pass), true
		}
	}
	return parts, nil
}

// unescapeLenient undoes percent-encoding in user and password. Invalid escape
// sequences pass through unchanged instead of erroring — a lone % in a password
// is common and nobody adds an extra escape layer for it.
func unescapeLenient(value string) string {
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return value
	}
	return decoded
}

// sqliteTarget parses the path after sqlite:///.
//
// Three versus four slashes is relative versus absolute: sqlite:///./data/x.db
// means ./data/x.db under the working directory, sqlite:////var/lib/x.db
// means /var/lib/x.db. As a string it is "drop :// then one more slash", so
// net/url is avoided to keep it from normalizing the path.
func sqliteTarget(rest string) (Target, error) {
	path, query, _ := strings.Cut(rest, "?")
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		path = ":memory:" // both sqlite:// and sqlite:///:memory: are in-memory
	}

	params, err := url.ParseQuery(query)
	if err != nil {
		return Target{}, fmt.Errorf("Invalid query parameters in the sqlite connection string: %w", err)
	}
	// the time format written back must be pinned: the driver default
	// (time.Time.String()) produces "2026-09-14 03:09:37.277711 +0000 UTC", which
	// neither matches existing rows nor compares sanely in SQLite's textual
	// timestamp >= ?. _time_format=sqlite writes ISO-8601, the same format as
	// existing data, so old and new rows compare and sort together.
	if !params.Has("_time_format") {
		params.Set("_time_format", "sqlite")
	}
	// SQLite's default busy_timeout is 0: the first write-lock collision returns
	// SQLITE_BUSY immediately. Five seconds of lock waiting keeps scheduled jobs
	// and page writes from erroring into each other.
	if !params.Has("_pragma") {
		params.Set("_pragma", "busy_timeout(5000)")
	}

	filePath := path
	if path == ":memory:" {
		filePath = ""
	}
	return Target{
		Engine:     EngineSQLite,
		DriverName: "sqlite",
		DSN:        path + "?" + params.Encode(),
		FilePath:   filePath,
	}, nil
}

// postgresTarget rebuilds a correctly escaped URL before handing it to pgx.
//
// The original cannot be passed through: pgx uses net/url internally too,
// and a # or ? in the password truncates it just the same.
func postgresTarget(rest string) (Target, error) {
	parts, err := parseConnURL(rest)
	if err != nil {
		return Target{}, fmt.Errorf("Failed to parse the postgresql connection string: %w", err)
	}

	target := url.URL{Scheme: "postgres", Path: "/" + parts.Database}
	if parts.User != "" {
		if parts.HasPass {
			target.User = url.UserPassword(parts.User, parts.Password)
		} else {
			target.User = url.User(parts.User)
		}
	}
	host := parts.Host
	if host == "" {
		host = "127.0.0.1"
	}
	if parts.Port != "" {
		host = net.JoinHostPort(host, parts.Port)
	}
	target.Host = host
	target.RawQuery = parts.Query

	return Target{Engine: EnginePostgres, DriverName: "pgx", DSN: target.String()}, nil
}

// mysqlTarget reassembles the URL into go-sql-driver's user:pass@tcp(host:port)/db?params form.
func mysqlTarget(rest string) (Target, error) {
	parts, err := parseConnURL(rest)
	if err != nil {
		return Target{}, fmt.Errorf("Failed to parse the mysql connection string: %w", err)
	}

	host := parts.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := parts.Port
	if port == "" {
		port = "3306"
	}

	// go-sql-driver DSNs skip percent-decoding; use the unescaped originals
	var credentials string
	if parts.User != "" {
		credentials = parts.User
		if parts.HasPass {
			credentials += ":" + parts.Password
		}
		credentials += "@"
	}

	params, err := url.ParseQuery(parts.Query)
	if err != nil {
		return Target{}, fmt.Errorf("Invalid query parameters in the mysql connection string: %w", err)
	}
	// time columns must come back as time.Time, not []byte, or every timestamp needs manual parsing
	if !params.Has("parseTime") {
		params.Set("parseTime", "true")
	}
	// UTC timestamps everywhere is a global convention; pin it here against driver default drift or edits
	if !params.Has("loc") {
		params.Set("loc", "UTC")
	}

	dsn := fmt.Sprintf("%stcp(%s)/%s", credentials, net.JoinHostPort(host, port), parts.Database)
	if encoded := params.Encode(); encoded != "" {
		dsn += "?" + encoded
	}
	return Target{Engine: EngineMySQL, DriverName: "mysql", DSN: dsn}, nil
}
