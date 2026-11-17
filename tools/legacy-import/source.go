package main

import (
	"bufio"
	"crypto/md5"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fernet/fernet-go"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// loadEnvFile reads KEY=VALUE lines the way the old version did: quotes
// around a value are stripped, and a # inside a value is part of it.
func loadEnvFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	env := map[string]string{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%s line %d is not KEY=VALUE", path, lineNo)
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' || value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
		env[strings.TrimSpace(key)] = value
	}
	return env, scanner.Err()
}

// The old DATABASE_URL is scheme://user:password@host:port/database?params,
// possibly with a "+driver" after the scheme. It is taken apart by hand:
// net/url reads a # or ? in a password as the start of something else.
var connURL = regexp.MustCompile(
	`^(?:(?P<user>[^:/@]*)(?::(?P<pass>[^@]*))?@)?` +
		`(?:\[(?P<v6>[^\]]+)\]|(?P<host>[^/:@]*))?` +
		`(?::(?P<port>[0-9]*))?` +
		`(?:/(?P<db>[^?]*))?` +
		`(?:\?(?P<query>.*))?$`)

func unescape(value string) string {
	if decoded, err := url.PathUnescape(value); err == nil {
		return decoded
	}
	return value
}

// openDatabase opens the old database read-only where the engine allows it.
func openDatabase(databaseURL string) (*sql.DB, string, error) {
	scheme, rest, ok := strings.Cut(strings.TrimSpace(databaseURL), "://")
	if !ok {
		return nil, "", errors.New(`the database URL is missing "://"`)
	}
	dialect, _, _ := strings.Cut(strings.ToLower(scheme), "+")
	if dialect == "sqlite" {
		path, _, _ := strings.Cut(rest, "?")
		path = strings.TrimPrefix(path, "/")
		if _, err := os.Stat(path); err != nil {
			return nil, "", fmt.Errorf("the SQLite file %s cannot be read: %w", path, err)
		}
		db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=busy_timeout(5000)")
		return db, "sqlite", err
	}

	m := connURL.FindStringSubmatch(rest)
	if m == nil {
		return nil, "", errors.New("the database URL is not in a recognised form")
	}
	group := func(name string) string { return m[connURL.SubexpIndex(name)] }
	host := group("host")
	if v6 := group("v6"); v6 != "" {
		host = v6
	}
	if host == "" {
		host = "127.0.0.1"
	}
	user, password := unescape(group("user")), unescape(group("pass"))

	switch dialect {
	case "mysql", "mariadb":
		port := group("port")
		if port == "" {
			port = "3306"
		}
		params, _ := url.ParseQuery(group("query"))
		params.Set("parseTime", "true")
		params.Set("loc", "UTC")
		credentials := ""
		if user != "" {
			credentials = user
			if password != "" {
				credentials += ":" + password
			}
			credentials += "@"
		}
		dsn := fmt.Sprintf("%stcp(%s)/%s?%s", credentials, net.JoinHostPort(host, port), group("db"), params.Encode())
		db, err := sql.Open("mysql", dsn)
		return db, "mysql", err
	case "postgresql", "postgres":
		target := url.URL{Scheme: "postgres", Path: "/" + group("db"), RawQuery: group("query")}
		if user != "" {
			target.User = url.UserPassword(user, password)
		}
		if port := group("port"); port != "" {
			host = net.JoinHostPort(host, port)
		}
		target.Host = host
		db, err := sql.Open("pgx", target.String())
		return db, "postgres", err
	}
	return nil, "", fmt.Errorf("unsupported database type %q", dialect)
}

// record is one row of an old table, by column name.
type record map[string]any

// table reads every row of a table. A table that does not exist reads as
// empty: old installs predate several of them.
func table(db *sql.DB, name, orderBy string) ([]record, error) {
	query := "SELECT * FROM " + name
	if orderBy != "" {
		query += " ORDER BY " + orderBy
	}
	rows, err := db.Query(query)
	if err != nil {
		text := strings.ToLower(err.Error())
		if strings.Contains(text, "no such table") || strings.Contains(text, "doesn't exist") || strings.Contains(text, "does not exist") {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []record
	for rows.Next() {
		values := make([]any, len(columns))
		targets := make([]any, len(columns))
		for i := range values {
			targets[i] = &values[i]
		}
		if err := rows.Scan(targets...); err != nil {
			return nil, err
		}
		row := record{}
		for i, column := range columns {
			row[strings.ToLower(column)] = values[i]
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r record) str(column string) string {
	switch v := r[column].(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case []byte:
		return strings.TrimSpace(string(v))
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func (r record) num(column string) (float64, bool) {
	switch v := r[column].(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int64:
		return float64(v), true
	case int:
		return float64(v), true
	case []byte:
		f, err := strconv.ParseFloat(strings.TrimSpace(string(v)), 64)
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f, err == nil
	}
	return 0, false
}

// truthy reads a boolean column; absent means the column's old default, yes.
func (r record) truthy(column string) bool {
	switch v := r[column].(type) {
	case nil:
		return true
	case bool:
		return v
	case int64:
		return v != 0
	case []byte:
		return string(v) != "0" && string(v) != "" && !strings.EqualFold(string(v), "false")
	case string:
		return v != "0" && v != "" && !strings.EqualFold(v, "false")
	}
	return true
}

var timeLayouts = []string{
	"2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999", "2006-01-02T15:04:05.999999999Z07:00",
	"2006-01-02T15:04:05.999999999", "2006-01-02 15:04:05", "2006-01-02",
}

// when reads a timestamp column. The old version stored UTC without a zone.
func (r record) when(column string) (time.Time, bool) {
	switch v := r[column].(type) {
	case time.Time:
		return v.UTC(), !v.IsZero()
	case []byte:
		return parseTime(string(v))
	case string:
		return parseTime(v)
	}
	return time.Time{}, false
}

func parseTime(text string) (time.Time, bool) {
	text = strings.TrimSpace(text)
	for _, layout := range timeLayouts {
		if at, err := time.ParseInLocation(layout, text, time.UTC); err == nil {
			return at.UTC(), true
		}
	}
	return time.Time{}, false
}

// ---------- the old encryption ----------

const encryptedPrefix = "enc:v1:"

// decrypter opens values the old version sealed with CONFIG_ENCRYPTION_KEY:
// a Fernet token behind an "enc:v1:" prefix.
type decrypter struct{ key *fernet.Key }

// newDecrypter derives the key exactly as the old version did: a setting
// that is itself a Fernet key is used directly, anything else is a
// passphrase whose SHA-256 is the key.
func newDecrypter(setting string) decrypter {
	raw := strings.TrimSpace(setting)
	if raw == "" {
		return decrypter{}
	}
	var key fernet.Key
	if decoded, ok := fernetKey(raw); ok {
		copy(key[:], decoded)
	} else {
		sum := sha256.Sum256([]byte(raw))
		copy(key[:], sum[:])
	}
	return decrypter{key: &key}
}

// fernetKey mirrors the old classification, quirks included: characters
// outside the alphabet are ignored and padding is required.
func fernetKey(raw string) ([]byte, bool) {
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '=':
			b.WriteRune(r)
		case r == '-', r == '+':
			b.WriteByte('+')
		case r == '_', r == '/':
			b.WriteByte('/')
		}
	}
	decoded, err := base64.StdEncoding.DecodeString(b.String())
	if err != nil || len(decoded) != 32 {
		return nil, false
	}
	return decoded, true
}

// open returns the plaintext of a stored value, and false when it is sealed
// and cannot be opened.
func (d decrypter) open(value string) (string, bool) {
	token, sealed := strings.CutPrefix(value, encryptedPrefix)
	if !sealed {
		return value, true
	}
	if d.key == nil {
		return "", false
	}
	plain := fernet.VerifyAndDecrypt([]byte(token), 0, []*fernet.Key{d.key})
	if plain == nil {
		return "", false
	}
	return string(plain), true
}

// oldProjectID is how the old version identified a project in its history
// tables: md5("provider:name"), with any "custom:" prefix dropped.
func oldProjectID(provider, name string) string {
	sum := md5.Sum([]byte(strings.TrimPrefix(provider, "custom:") + ":" + name))
	return hex.EncodeToString(sum[:])
}

// oldSubscriptionID is md5("subscription:name").
func oldSubscriptionID(name string) string {
	sum := md5.Sum([]byte("subscription:" + name))
	return hex.EncodeToString(sum[:])
}
