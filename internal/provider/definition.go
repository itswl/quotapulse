// Package provider reads meters from the platforms that hold them.
//
// A platform is described by a Definition: one HTTP request and one jq
// program that turns the response into meters. That is the whole contract —
// adding a platform never means writing Go. The common ones ship in the
// catalog; anything else is a definition saved from the dashboard.
//
// One request can yield several meters. A coding plan reports a five-hour
// window and a monthly tool quota in the same response; a multi-currency
// account has a balance per currency. Each becomes its own meter with its own
// history and rules.
package provider

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Auth types a definition can ask for.
const (
	AuthBearer    = "bearer"     // Authorization: Bearer <value>
	AuthHeader    = "header"     // <name>: <value>
	AuthQuery     = "query"      // ?<name>=<value>
	AuthBasic     = "basic"      // Authorization: Basic base64(<value>), value is "user:password"
	AuthNone      = "none"       // the credential travels in the URL, a header or the body by template
	AuthAliyunRPC = "aliyun-rpc" // Alibaba Cloud RPC signature over the query
	AuthVolcV4    = "volc-v4"    // Volcengine V4 signature over the request
)

// Field is something the user fills in when adding an account.
type Field struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Help        string `json:"help,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	// Secret fields are write-only: the API never returns them.
	Secret   bool   `json:"secret,omitempty"`
	Optional bool   `json:"optional,omitempty"`
	Default  string `json:"default,omitempty"`
}

// Auth says how the credential reaches the platform.
type Auth struct {
	Type string `json:"type"`
	// Name is the header or query parameter for the header and query types.
	Name string `json:"name,omitempty"`
	// Value is a template; "{{api_key}}" when empty.
	Value string `json:"value,omitempty"`
	// Region and Service scope a volc-v4 signature.
	Region  string `json:"region,omitempty"`
	Service string `json:"service,omitempty"`
}

// Request is the one call a definition makes. URL, headers, query values and
// body are templates: {{field}} is replaced by the account's field of that
// name, and a few built-ins ({{now}}, {{today}}, {{month_start}}) are always
// there.
type Request struct {
	Method  string            `json:"method,omitempty"` // GET when empty
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Query   map[string]string `json:"query,omitempty"`
	Body    string            `json:"body,omitempty"`
	Auth    Auth              `json:"auth"`
}

// How a built-in definition was checked. Stated in the catalog so nobody has
// to guess how much to trust an entry.
const (
	CheckedLive     = "live"     // run against the real API
	CheckedRecorded = "recorded" // replays responses recorded from the real API
	CheckedDocs     = "docs"     // written from the platform's documentation
)

// Definition describes how to read one platform.
type Definition struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	NameZH   string  `json:"name_zh,omitempty"`
	Homepage string  `json:"homepage,omitempty"`
	Notes    string  `json:"notes,omitempty"`
	NotesZH  string  `json:"notes_zh,omitempty"`
	Fields   []Field `json:"fields"`
	Request  Request `json:"request"`
	// Extract is a jq program. Its input is the response body; each object it
	// outputs is a meter:
	//
	//	{key, name, name_zh, kind, unit, value, limit, resets_at, window}
	//
	// Only value is required. Calling error("…") fails the sample with that
	// message, which is how an API that answers 200 with an error inside is
	// handled.
	Extract string `json:"extract"`

	Builtin bool   `json:"builtin"`
	Checked string `json:"checked,omitempty"`
}

var (
	idPattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,39}$`)
	fieldKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	placeholder     = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_]+)\s*\}\}`)
)

// Template variables every request can use.
var builtinVars = map[string]bool{"now": true, "today": true, "month_start": true, "month_start_date": true}

const maxFields = 12

// Validate reports everything wrong with a definition.
func (d Definition) Validate() error {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	if !idPattern.MatchString(d.ID) {
		add("id: lowercase letters, digits and dashes, 2 to 40 characters")
	}
	if strings.TrimSpace(d.Name) == "" {
		add("name: required")
	}

	known := map[string]bool{}
	if len(d.Fields) > maxFields {
		add("fields: at most %d", maxFields)
	}
	for _, f := range d.Fields {
		switch {
		case !fieldKeyPattern.MatchString(f.Key):
			add("fields: %q is not a valid key (lowercase letters, digits, underscores)", f.Key)
		case builtinVars[f.Key]:
			add("fields: %q is a built-in template variable", f.Key)
		case known[f.Key]:
			add("fields: %q is declared twice", f.Key)
		}
		known[f.Key] = true
	}

	switch strings.ToUpper(d.Request.Method) {
	case "", "GET", "POST":
	default:
		add("request.method: GET or POST")
	}
	rawURL := strings.TrimSpace(d.Request.URL)
	if rawURL == "" {
		add("request.url: required")
	} else if !strings.HasPrefix(rawURL, "{{") {
		// A URL that starts with a template is a base URL the user supplies;
		// it is checked when the request is built.
		if u, err := url.Parse(placeholder.ReplaceAllString(rawURL, "x")); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			add("request.url: must be an http(s) URL")
		}
	}

	auth := d.Request.Auth
	switch auth.Type {
	case AuthBearer, AuthBasic, AuthNone:
	case AuthHeader, AuthQuery:
		if strings.TrimSpace(auth.Name) == "" {
			add("request.auth.name: required for %s auth", auth.Type)
		}
	case AuthAliyunRPC:
		for _, key := range []string{"access_key_id", "access_key_secret"} {
			if !known[key] {
				add("fields: aliyun-rpc auth needs a field named %s", key)
			}
		}
	case AuthVolcV4:
		for _, key := range []string{"access_key_id", "secret_access_key"} {
			if !known[key] {
				add("fields: volc-v4 auth needs a field named %s", key)
			}
		}
		if auth.Region == "" || auth.Service == "" {
			add("request.auth: volc-v4 needs a region and a service")
		}
	default:
		add("request.auth.type: one of bearer, header, query, basic, none, aliyun-rpc, volc-v4")
	}
	if auth.Type == AuthBearer || auth.Type == AuthHeader || auth.Type == AuthQuery || auth.Type == AuthBasic {
		if auth.Value == "" && !known["api_key"] {
			add("request.auth.value: required unless there is a field named api_key")
		}
	}

	for _, text := range d.templates() {
		for _, match := range placeholder.FindAllStringSubmatch(text, -1) {
			if name := match[1]; !known[name] && !builtinVars[name] {
				add("{{%s}} is not a declared field", name)
			}
		}
	}

	if strings.TrimSpace(d.Extract) == "" {
		add("extract: required")
	} else if _, err := compile(d.Extract); err != nil {
		add("extract: %v", err)
	}

	if len(problems) == 0 {
		return nil
	}
	return errors.New(strings.Join(problems, "; "))
}

// templates lists every string of the request that may hold placeholders.
func (d Definition) templates() []string {
	out := []string{d.Request.URL, d.Request.Body, d.Request.Auth.Value}
	for _, v := range d.Request.Headers {
		out = append(out, v)
	}
	for _, v := range d.Request.Query {
		out = append(out, v)
	}
	return out
}

// SecretKeys lists the fields whose values must never be shown again.
func (d Definition) SecretKeys() map[string]bool {
	out := map[string]bool{}
	for _, f := range d.Fields {
		if f.Secret {
			out[f.Key] = true
		}
	}
	return out
}

// CheckCredential reports which required fields a credential is missing, and
// fills in defaults.
func (d Definition) CheckCredential(credential map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(d.Fields))
	var missing []string
	for _, f := range d.Fields {
		value := strings.TrimSpace(credential[f.Key])
		if value == "" {
			value = f.Default
		}
		if value == "" && !f.Optional {
			label := f.Label
			if label == "" {
				label = f.Key
			}
			missing = append(missing, label)
			continue
		}
		if value != "" {
			out[f.Key] = value
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing %s", strings.Join(missing, ", "))
	}
	return out, nil
}

// MaskCredential is what the API shows of a credential: plain fields as they
// are, secret fields as a hint that something is stored.
func (d Definition) MaskCredential(credential map[string]string) map[string]string {
	secrets := d.SecretKeys()
	out := make(map[string]string, len(credential))
	for key, value := range credential {
		if secrets[key] {
			out[key] = Mask(value)
		} else {
			out[key] = value
		}
	}
	return out
}

// Mask keeps the ends of a secret so two keys can be told apart, and nothing
// more.
func Mask(secret string) string {
	runes := []rune(secret)
	if len(runes) <= 8 {
		return "••••"
	}
	return string(runes[:3]) + "••••" + string(runes[len(runes)-4:])
}

// Redact removes secret values from a text bound for a log, an incident or
// an API response. Platforms like to echo the key they rejected.
func Redact(text string, secrets ...string) string {
	for _, secret := range secrets {
		if len(secret) >= 6 {
			text = strings.ReplaceAll(text, secret, "•••")
		}
	}
	return text
}
