package provider

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/itswl/quotapulse/internal/model"
)

// ---------- Dynamic (page-defined) providers ----------
//
// Custom providers live in the database and are registered into the same
// registry as the built-in ones under the "custom:<name>" namespace. The
// resolver replaces the whole set on every load, so edits on the page take
// effect on the next check round without a restart.

var (
	registryMu     sync.RWMutex
	dynamicSpecs   = map[string]Spec{}
	dynamicPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,30}$`)
)

// ReplaceDynamic swaps the whole dynamic set. Builtin keys are never
// affected; a custom key colliding with a builtin is rejected.
func ReplaceDynamic(specs []Spec) error {
	registryMu.Lock()
	defer registryMu.Unlock()
	next := make(map[string]Spec, len(specs))
	for _, spec := range specs {
		if _, builtin := registry[spec.Key]; builtin {
			return fmt.Errorf("provider key %q collides with a built-in platform", spec.Key)
		}
		next[spec.Key] = spec
	}
	dynamicSpecs = next
	return nil
}

func lookupSpec(key string) (Spec, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	if spec, ok := dynamicSpecs[key]; ok {
		return spec, true
	}
	return Spec{}, false
}

func dynamicKeys() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	keys := make([]string, 0, len(dynamicSpecs))
	for k := range dynamicSpecs {
		keys = append(keys, k)
	}
	return keys
}

// IsDynamic reports whether key belongs to a page-defined provider.
func IsDynamic(key string) bool {
	registryMu.RLock()
	defer registryMu.RUnlock()
	_, ok := dynamicSpecs[key]
	return ok
}

// ---------- JSON path access ----------

// DigPath walks a dotted path with numeric array indexes and element filters,
// e.g. "data.items.0.balance" or "accounts.{currency:CNY}.total". A filter
// token {field:value} picks the first array element whose field matches the
// value (strings compare verbatim; numbers compare numerically); when no
// element matches, the first element is used as the fallback. Returns nil
// when any level is missing.
func DigPath(data map[string]any, path string) any {
	var current any = data
	for _, token := range splitPath(path) {
		token = strings.TrimSpace(token)
		if token == "" {
			return nil
		}
		if filter, ok := strings.CutPrefix(strings.TrimSpace(token), "{"); ok {
			spec, ok := strings.CutSuffix(filter, "}")
			if !ok {
				return nil
			}
			current = pickElement(current, spec)
			continue
		}
		if index, err := strconv.Atoi(token); err == nil {
			arr, ok := current.([]any)
			if !ok || index < 0 || index >= len(arr) {
				return nil
			}
			current = arr[index]
			continue
		}
		obj, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = obj[token]
	}
	return current
}

// splitPath splits on dots, keeping {…} filters intact.
func splitPath(path string) []string {
	var tokens []string
	var current strings.Builder
	inFilter := false
	for _, r := range strings.TrimSpace(path) {
		switch {
		case r == '{':
			inFilter = true
			current.WriteRune(r)
		case r == '}':
			inFilter = false
			current.WriteRune(r)
		case r == '.' && !inFilter:
			tokens = append(tokens, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	return append(tokens, current.String())
}

// pickElement finds the first array element whose field matches the value;
// without a match the first element stands in, mirroring the built-in
// DeepSeek adapter's "prefer CNY, else the first account" rule.
func pickElement(current any, spec string) any {
	field, value, found := strings.Cut(spec, ":")
	if !found {
		return nil
	}
	arr, ok := current.([]any)
	if !ok || len(arr) == 0 {
		return nil
	}
	var fallback any = arr[0]
	for _, item := range arr {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		switch candidate := obj[field].(type) {
		case string:
			if candidate == value {
				return item
			}
		default:
			if got, ok := Num(candidate); ok {
				if want, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil && got == want {
					return item
				}
			}
		}
	}
	return fallback
}

// DigFirst walks the pipe-separated alternatives and returns the first
// non-nil hit, e.g. "user_data.balance | data.balance | balance".
func DigFirst(data map[string]any, paths string) any {
	for _, path := range strings.Split(paths, "|") {
		if found := DigPath(data, path); found != nil {
			return found
		}
	}
	return nil
}

// numLoose parses a path value into a number. Clean numbers (including
// thousands separators) go through Num; a longer string is scanned for its
// first embedded number, e.g. "剩余263419余额" → 263419 or "1,234.56 元" →
// 1234.56. This mirrors the built-in wxrank adapter, whose API hides the
// balance inside a human sentence.
func numLoose(v any) (float64, bool) {
	if got, ok := Num(v); ok {
		return got, true
	}
	text, ok := v.(string)
	if !ok {
		return 0, false
	}
	match := firstNumber.FindString(text)
	if match == "" {
		return 0, false
	}
	return Num(match)
}

// firstNumber matches a number with optional sign, thousands separators and
// decimals — but not a bare run of punctuation (",,,"), mirroring the
// mailscan amount rules.
var firstNumber = regexp.MustCompile(`-?\d[\d,]*(?:\.\d+)?`)

// ---------- Spec construction ----------

// CustomProviderAuth lists the accepted auth_mode spellings.
const (
	CustomAuthBearer    = "bearer"
	CustomAuthQuery     = "query"
	CustomAuthHeader    = "header"
	CustomAuthNone      = "none"
	CustomAuthAliyunRPC = "aliyun_rpc"
	CustomAuthVolcV4    = "volc_v4"
)

// ValidateCustomProvider returns every problem with a page-defined provider
// configuration, in the order the fields appear on the form.
func ValidateCustomProvider(p model.CustomProvider) []string {
	var problems []string
	name := strings.ToLower(strings.TrimSpace(p.Name))
	if !dynamicPattern.MatchString(name) {
		problems = append(problems, "name: lowercase letters, digits and dashes only, 2-31 characters")
	}
	if u, err := url.Parse(strings.TrimSpace(p.URL)); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		problems = append(problems, "url: must be a valid http(s) URL")
	}
	switch p.AuthMode {
	case CustomAuthBearer, CustomAuthNone:
	case CustomAuthQuery, CustomAuthHeader:
		if strings.TrimSpace(p.AuthParam) == "" {
			problems = append(problems, "auth_param: required for query and header auth modes")
		}
	case CustomAuthAliyunRPC, CustomAuthVolcV4:
	default:
		problems = append(problems, "auth_mode: one of bearer, query, header, none, aliyun_rpc, volc_v4")
	}
	if p.AuthMode == CustomAuthVolcV4 {
		if strings.TrimSpace(p.SignRegion) == "" || strings.TrimSpace(p.SignService) == "" {
			problems = append(problems, "sign_region and sign_service: required for volc_v4 signing")
		}
	}
	switch p.DefaultType {
	case model.TypeBalance, model.TypeCredits, model.TypeQuota:
	default:
		problems = append(problems, "default_type: one of balance, credits, quota")
	}
	if p.DefaultType != model.TypeQuota {
		currency := strings.ToUpper(strings.TrimSpace(p.Currency))
		if currency != "" && len(currency) != 3 {
			problems = append(problems, "currency: a 3-letter ISO code (e.g. USD, CNY) or empty")
		}
	}
	if strings.TrimSpace(p.BalancePath) == "" {
		problems = append(problems, "balance_path: required, e.g. data.balance")
	}
	if p.BalanceScale < 0 {
		problems = append(problems, "balance_scale: cannot be negative")
	}
	switch p.ExtractMode {
	case "", "path", "min_window":
	default:
		problems = append(problems, "extract_mode: path or min_window")
	}
	if p.WindowResetHours < 0 || p.WindowResetHours > 168 {
		problems = append(problems, "window_reset_hours: 0-168 (0 means the 6-hour default)")
	}
	for _, pair := range splitTrim(p.ExtraParams) {
		if _, _, found := strings.Cut(pair, "="); !found {
			problems = append(problems, "extra_params: entries look like key=value")
			break
		}
	}
	return problems
}

// SpecFromCustomProvider builds a runtime Spec. Call ValidateCustomProvider
// first; this never fails on validated input but stays defensive.
func SpecFromCustomProvider(p model.CustomProvider) (Spec, error) {
	if problems := ValidateCustomProvider(p); len(problems) > 0 {
		return Spec{}, fmt.Errorf("invalid custom provider: %s", strings.Join(problems, "; "))
	}
	name := strings.ToLower(strings.TrimSpace(p.Name))
	display := strings.TrimSpace(p.DisplayName)
	if display == "" {
		display = name
	}
	scale := p.BalanceScale
	if scale == 0 {
		scale = 1
	}
	spec := Spec{
		Key:         model.CustomProviderKey(name),
		Name:        display,
		DefaultType: p.DefaultType,
		Currency:    strings.ToUpper(strings.TrimSpace(p.Currency)),
		URL:         strings.TrimSpace(p.URL),
		Params:      parseKeyValue(p.ExtraParams),
	}
	switch p.AuthMode {
	case CustomAuthQuery:
		spec.Auth = AuthQuery
		spec.AuthParam = strings.TrimSpace(p.AuthParam)
	case CustomAuthHeader:
		spec.Auth = AuthHeader
		spec.AuthParam = strings.TrimSpace(p.AuthParam)
	case CustomAuthNone:
		spec.Auth = AuthNone
	case CustomAuthAliyunRPC:
		spec.Auth = AuthNone
		spec.Sign = aliyunRPCSigner(p)
	case CustomAuthVolcV4:
		spec.Auth = AuthNone
		spec.Sign = volcV4Signer(p)
	}
	if strings.TrimSpace(p.ExtractMode) == "min_window" {
		spec.Extract = minWindowExtract(p)
	} else {
		spec.Extract = pathExtract(p, scale)
	}
	spec.Check = customCheck(p)
	return spec, nil
}

// pathExtract is the default pipeline: the first alternative path that holds
// a number wins, an optional second path is subtracted (top-ups minus spend),
// and the scale normalizes units.
func pathExtract(p model.CustomProvider, scale float64) func(map[string]any) (float64, error) {
	balancePath := strings.TrimSpace(p.BalancePath)
	subtractPath := strings.TrimSpace(p.SubtractPath)
	return func(data map[string]any) (float64, error) {
		raw := DigFirst(data, balancePath)
		if raw == nil {
			return 0, fmt.Errorf("balance not found at %s", balancePath)
		}
		value, ok := numLoose(raw)
		if !ok {
			return 0, fmt.Errorf("no number at %s", balancePath)
		}
		if subtractPath != "" {
			used, ok := numLoose(DigFirst(data, subtractPath))
			if !ok {
				return 0, fmt.Errorf("no number at %s", subtractPath)
			}
			value -= used
		}
		return round2(value * scale), nil
	}
}

// minWindowExtract reports the lowest remaining percentage across the quota
// windows that reset soon (a rolling window by definition) — the GLM Coding
// Plan shape, shared by plan-style quotas like Claude's or Copilot's.
func minWindowExtract(p model.CustomProvider) func(map[string]any) (float64, error) {
	windowTypes := splitTrim(p.WindowTypes)
	if len(windowTypes) == 0 {
		windowTypes = []string{"TOKENS_LIMIT", "CREDIT_LIMIT"}
	}
	resetHours := p.WindowResetHours
	if resetHours == 0 {
		resetHours = 6
	}
	return func(data map[string]any) (float64, error) {
		limits, ok := Dig(data, "data", "limits").([]any)
		if !ok {
			return 0, errors.New("Could not parse data.limits field")
		}
		now := time.Now()
		var rolling, other []float64
		for _, item := range limits {
			limit := Object(item)
			if limit == nil || !windowOfType(limit, windowTypes) {
				continue
			}
			percent, ok := glmRemainingPercent(limit)
			if !ok {
				continue
			}
			if glmResetsWithin(limit, now, time.Duration(resetHours*float64(time.Hour))) {
				rolling = append(rolling, percent)
			} else {
				other = append(other, percent)
			}
		}
		if len(rolling) > 0 {
			return round2(minFloat(rolling)), nil
		}
		if len(other) > 0 {
			return round2(minFloat(other)), nil
		}
		return 0, errors.New("data.limits contains no quota window")
	}
}

func windowOfType(limit map[string]any, types []string) bool {
	got, _ := limit["type"].(string)
	if got == "" {
		return true // unlabelled legacy entries qualify
	}
	for _, want := range types {
		if got == want {
			return true
		}
	}
	return false
}

// aliyunRPCSigner signs every request with the Aliyun RPC preset: the key is
// "AccessKeyId:Secret", Action/Version come from extra_params (defaulting to
// the business-account balance query).
func aliyunRPCSigner(p model.CustomProvider) func(string, string) (map[string]string, map[string]string, error) {
	params := parseKeyValue(p.ExtraParams)
	action := params["Action"]
	if action == "" {
		action = "QueryAccountBalance"
	}
	version := params["Version"]
	if version == "" {
		version = "2017-12-14"
	}
	return func(rawURL, apiKey string) (map[string]string, map[string]string, error) {
		id, secret, err := SplitKeyPair(apiKey, "custom Aliyun RPC", "AccessKeyId:AccessKeySecret")
		if err != nil {
			return nil, nil, err
		}
		signed := AliyunRPCParams(id, secret, action, version, params, time.Now(), aliyunNonce())
		return nil, signed, nil
	}
}

// volcV4Signer signs every request Volcengine V4 style: the key is "AK:SK",
// host and path derive from the endpoint URL, region and service come from
// the definition.
func volcV4Signer(p model.CustomProvider) func(string, string) (map[string]string, map[string]string, error) {
	region := strings.TrimSpace(p.SignRegion)
	service := strings.TrimSpace(p.SignService)
	params := parseKeyValue(p.ExtraParams)
	return func(rawURL, apiKey string) (map[string]string, map[string]string, error) {
		ak, sk, err := SplitKeyPair(apiKey, "custom Volcengine V4", "AK:SK")
		if err != nil {
			return nil, nil, err
		}
		host, path, err := hostAndPathOf(rawURL)
		if err != nil {
			return nil, nil, err
		}
		headers := VolcV4Headers(VolcV4Request{
			Host: host, Path: path, Region: region, Service: service,
			AK: ak, SK: sk, Params: params,
		}, time.Now())
		return headers, params, nil
	}
}

// parseKeyValue reads "k=v,k2=v2" into a map; malformed entries are dropped
// (validation reports them first).
func parseKeyValue(raw string) map[string]string {
	out := map[string]string{}
	for _, pair := range splitTrim(raw) {
		key, value, found := strings.Cut(pair, "=")
		if found {
			out[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return out
}

// customCheck builds the business-level validator from success_path /
// success_values and the error message path.
func customCheck(p model.CustomProvider) func(map[string]any) error {
	successPath := strings.TrimSpace(p.SuccessPath)
	successValues := splitTrim(p.SuccessValues)
	errorPath := strings.TrimSpace(p.ErrorPath)
	return func(data map[string]any) error {
		if successPath != "" {
			got := DigPath(data, successPath)
			if got == nil {
				return fmt.Errorf("success field not found at %s", successPath)
			}
			if len(successValues) > 0 {
				text := formatValue(got)
				for _, want := range successValues {
					if text == want {
						return nil
					}
				}
				return fmt.Errorf("API returned an error: %s", customErrorText(data, errorPath, text))
			}
			// no accepted values configured: truthiness decides
			if !truthy(got) {
				return fmt.Errorf("API returned an error: %s", customErrorText(data, errorPath, formatValue(got)))
			}
		}
		return nil
	}
}

func customErrorText(data map[string]any, errorPath, fallback string) string {
	if errorPath != "" {
		if message := DigPath(data, errorPath); message != nil {
			if text := formatValue(message); text != "" {
				return text
			}
		}
	}
	return fallback
}

func splitTrim(raw string) []string {
	var out []string
	for _, item := range strings.Split(raw, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// NewFromSpec builds a one-shot adapter for a spec that is not (yet)
// registered — the wiring-test endpoint uses it to prove a definition works
// before anything is saved.
func NewFromSpec(spec Spec, apiKey string, client *Client) Provider {
	if client == nil {
		client = NewClient(DefaultTimeout)
	}
	return &specProvider{spec: spec, apiKey: apiKey, client: client}
}
