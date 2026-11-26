package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/itswl/quotapulse/internal/core"
	"github.com/itswl/quotapulse/internal/provider"
)

// The old version let a platform be described by a row of settings: a URL, a
// dotted path to the balance, a few switches. 1.0 describes a platform with
// one request and a jq program. This file translates the former into the
// latter.

// seeded are the definitions the old version created by itself. They are
// built into 1.0, so their rows are not converted.
var seeded = map[string]bool{
	"deepseek": true, "glm": true, "openrouter": true, "tikhub": true, "uniapi": true,
	// wxrank is no longer offered: its old row is still not converted, and
	// an account that used it is reported as not migrated.
	"wxrank": true, "volc": true, "aliyun": true,
}

func (c *converter) providers() error {
	if c.db == nil {
		return nil
	}
	rows, err := table(c.db, "custom_provider_config", "id")
	if err != nil {
		return err
	}
	for _, r := range rows {
		name := strings.ToLower(r.str("name"))
		if seeded[name] {
			continue
		}
		def, err := convertDefinition(r)
		if err != nil {
			c.note("custom platform %q could not be converted (%v); define it again under Settings → Providers", name, err)
			continue
		}
		if _, clash := provider.Builtin(def.ID); clash {
			def.ID += "-custom"
		}
		if err := def.Validate(); err != nil {
			c.note("custom platform %q could not be converted (%v); define it again under Settings → Providers", name, err)
			continue
		}
		c.custom[name] = def
		c.out.Providers = append(c.out.Providers, def)
	}
	return nil
}

func convertDefinition(r record) (provider.Definition, error) {
	name := strings.ToLower(r.str("name"))
	def := provider.Definition{
		ID: name, Name: r.str("display_name"),
		Fields:  []provider.Field{{Key: "api_key", Label: "API Key", Secret: true}},
		Request: provider.Request{URL: r.str("url"), Query: keyValues(r.str("extra_params"))},
	}
	if def.Name == "" {
		def.Name = name
	}

	switch mode := r.str("auth_mode"); mode {
	case "bearer", "":
		def.Request.Auth = provider.Auth{Type: provider.AuthBearer}
	case "query":
		def.Request.Auth = provider.Auth{Type: provider.AuthQuery, Name: r.str("auth_param")}
	case "header":
		def.Request.Auth = provider.Auth{Type: provider.AuthHeader, Name: r.str("auth_param")}
	case "none":
		def.Request.Auth = provider.Auth{Type: provider.AuthNone}
	case "aliyun_rpc":
		def.Request.Auth = provider.Auth{Type: provider.AuthAliyunRPC}
		def.Fields = []provider.Field{{Key: "access_key_id", Label: "AccessKey ID"}, {Key: "access_key_secret", Label: "AccessKey Secret", Secret: true}}
		// The old preset filled these in when the row did not.
		if def.Request.Query == nil {
			def.Request.Query = map[string]string{}
		}
		if def.Request.Query["Action"] == "" {
			def.Request.Query["Action"] = "QueryAccountBalance"
		}
		if def.Request.Query["Version"] == "" {
			def.Request.Query["Version"] = "2017-12-14"
		}
	case "volc_v4":
		def.Request.Auth = provider.Auth{Type: provider.AuthVolcV4, Region: r.str("sign_region"), Service: r.str("sign_service")}
		def.Fields = []provider.Field{{Key: "access_key_id", Label: "Access Key ID"}, {Key: "secret_access_key", Label: "Secret Access Key", Secret: true}}
	default:
		return def, fmt.Errorf("unknown auth mode %q", mode)
	}

	kind, unit := "balance", strings.ToUpper(r.str("currency"))
	if r.str("default_type") == "quota" {
		kind, unit = "quota", "%"
	}

	// The window mode was written for one response shape — a list of quota
	// windows, the lowest of the soon-to-reset ones wins — and 1.0 reads
	// that shape as separate meters.
	if r.str("extract_mode") == "min_window" {
		builtin, _ := provider.Builtin("glm-coding")
		def.Extract = builtin.Extract
		return def, nil
	}

	value := "(" + alternatives(r.str("balance_path")) + " | num)"
	if subtract := r.str("subtract_path"); subtract != "" {
		value = "(" + value + " - (" + alternatives(subtract) + " | num))"
	}
	if scale, ok := r.num("balance_scale"); ok && scale != 0 && scale != 1 {
		value = "(" + value + " * " + strconv.FormatFloat(scale, 'g', -1, 64) + ")"
	}
	limit := ""
	if kind == "quota" {
		limit = ", limit: 100"
	}
	object := fmt.Sprintf(`{key: "main", kind: %q, unit: %q, value: %s%s}`, kind, unit, value, limit)

	// success_path, when set, had to hold one of success_values — or, with
	// no values listed, anything truthy.
	if success := r.str("success_path"); success != "" {
		message := `"the request was rejected"`
		if errorPath := r.str("error_path"); errorPath != "" {
			message = "((" + path(errorPath) + " | tostring?) // " + message + ")"
		}
		test := "(" + path(success) + " | truthy)"
		if values := splitTrim(r.str("success_values")); len(values) > 0 {
			var options []string
			for _, v := range values {
				options = append(options, strconv.Quote(v))
			}
			test = "((" + path(success) + " | tostring) as $s | [" + strings.Join(options, ", ") + "] | index($s) != null)"
		}
		def.Extract = "def truthy: . != null and . != false and . != 0 and . != \"\";\nif " + test + " then " + object + " else error(" + message + ") end"
	} else {
		def.Extract = object
	}
	return def, nil
}

// alternatives turns "a.b | c.d" — try each path, take the first that is
// there — into jq's own alternative operator.
func alternatives(paths string) string {
	var out []string
	for _, p := range strings.Split(paths, "|") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, path(p))
		}
	}
	if len(out) == 0 {
		return "null"
	}
	return "(" + strings.Join(out, " // ") + ")"
}

// path turns one dotted path into jq. A number indexes an array; {field:value}
// picks the first element whose field matches, or the first element at all.
func path(dotted string) string {
	out := "."
	first := true
	for _, token := range splitPath(dotted) {
		token = strings.TrimSpace(token)
		step := ""
		switch {
		case token == "":
			continue
		case strings.HasPrefix(token, "{") && strings.HasSuffix(token, "}"):
			field, value, _ := strings.Cut(token[1:len(token)-1], ":")
			// Anything that is not a list has no element to pick: null, as
			// the old path engine answered.
			step = fmt.Sprintf("(if type == \"array\" then ((map(select(type == \"object\" and ((.[%s] | tostring) == %s))) | first) // first) else null end)",
				strconv.Quote(field), strconv.Quote(value))
		default:
			if index, err := strconv.Atoi(token); err == nil {
				step = fmt.Sprintf(".[%d]?", index)
			} else {
				step = fmt.Sprintf(".[%s]?", strconv.Quote(token))
			}
		}
		if first {
			if strings.HasPrefix(step, ".") {
				out = step
			} else {
				out = ". | " + step
			}
			first = false
			continue
		}
		out += " | " + step
	}
	return "(" + out + ")"
}

// splitPath splits on dots, keeping a {…} filter whole.
func splitPath(dotted string) []string {
	var tokens []string
	var current strings.Builder
	depth := 0
	for _, r := range strings.TrimSpace(dotted) {
		switch {
		case r == '{':
			depth++
			current.WriteRune(r)
		case r == '}':
			depth--
			current.WriteRune(r)
		case r == '.' && depth == 0:
			tokens = append(tokens, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	return append(tokens, current.String())
}

func splitTrim(csv string) []string {
	var out []string
	for _, item := range strings.Split(csv, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func keyValues(csv string) map[string]string {
	out := map[string]string{}
	for _, pair := range splitTrim(csv) {
		if key, value, ok := strings.Cut(pair, "="); ok {
			out[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func customKind(def provider.Definition) core.Kind {
	if strings.Contains(def.Extract, `kind: "quota"`) {
		return core.Quota
	}
	return core.Balance
}

func customUnit(def provider.Definition) string {
	_, rest, ok := strings.Cut(def.Extract, `unit: "`)
	if !ok {
		return ""
	}
	unit, _, _ := strings.Cut(rest, `"`)
	return unit
}

// credentialFor shapes an old single-string key into a definition's fields.
func credentialFor(def provider.Definition, apiKey string) map[string]string {
	if len(def.Fields) == 2 {
		id, secret, ok := strings.Cut(apiKey, ":")
		if ok {
			return map[string]string{def.Fields[0].Key: id, def.Fields[1].Key: secret}
		}
	}
	return map[string]string{"api_key": apiKey}
}
