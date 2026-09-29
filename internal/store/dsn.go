package store

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

// Implementation note.
type Engine string

const (
	EngineSQLite   Engine = "sqlite"
	EnginePostgres Engine = "postgres"
	EngineMySQL    Engine = "mysql"
)

// Implementation note.
//
// Implementation note.
// Implementation note.
type Target struct {
	Engine     Engine
	DriverName string // database/sql operation
	DSN        string
	FilePath   string // operation SQLite:operation,Open operation
}

// Implementation note.
func ParseURL(databaseURL string) (Target, error) {
	raw := strings.TrimSpace(databaseURL)
	if raw == "" {
		return Target{}, fmt.Errorf("DATABASE_URL is empty")
	}

	scheme, rest, ok := strings.Cut(raw, "://")
	if !ok {
		return Target{}, fmt.Errorf("DATABASE_URL is missing \"://\": %q", databaseURL)
	}
	// Implementation note.
	// Implementation note.
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

// Implementation note.
type urlParts struct {
	User     string
	Password string
	HasPass  bool
	Host     string
	Port     string
	Database string
	Query    string
}

// Implementation note.
//
// Implementation note.
// Implementation note.
// Implementation note.
var connURLPattern = regexp.MustCompile(
	`^(?:(?P<user>[^:/@]*)(?::(?P<pass>[^@]*))?@)?` + // operation,operation @ operation
		`(?:\[(?P<v6>[^\]]+)\]|(?P<host>[^/:@]*))?` + // operation,operation [::1] operation
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
	// Implementation note.
	if idx := strings.Index(rest, "@"); idx >= 0 {
		credentials := rest[:idx]
		if _, pass, ok := strings.Cut(credentials, ":"); ok {
			parts.Password, parts.HasPass = unescapeLenient(pass), true
		}
	}
	return parts, nil
}

// Implementation note.
// Implementation note.
// Implementation note.
func unescapeLenient(value string) string {
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return value
	}
	return decoded
}

// Implementation note.
//
// Implementation note.
// Implementation note.
// Implementation note.
func sqliteTarget(rest string) (Target, error) {
	path, query, _ := strings.Cut(rest, "?")
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		path = ":memory:" // sqlite:// operation sqlite:///:memory: operation
	}

	params, err := url.ParseQuery(query)
	if err != nil {
		return Target{}, fmt.Errorf("Invalid query parameters in the sqlite connection string: %w", err)
	}
	// Implementation note.
	// Implementation note.
	// Implementation note.
	// Implementation note.
	if !params.Has("_time_format") {
		params.Set("_time_format", "sqlite")
	}
	// Implementation note.
	// Implementation note.
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

// Implementation note.
//
// Implementation note.
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

// Implementation note.
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

	// Implementation note.
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
	// Implementation note.
	if !params.Has("parseTime") {
		params.Set("parseTime", "true")
	}
	// Implementation note.
	if !params.Has("loc") {
		params.Set("loc", "UTC")
	}

	dsn := fmt.Sprintf("%stcp(%s)/%s", credentials, net.JoinHostPort(host, port), parts.Database)
	if encoded := params.Encode(); encoded != "" {
		dsn += "?" + encoded
	}
	return Target{Engine: EngineMySQL, DriverName: "mysql", DSN: dsn}, nil
}
