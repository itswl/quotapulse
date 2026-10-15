package provider

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

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

// DigPath walks a dotted path with numeric array indexes, e.g.
// "data.items.0.balance". Returns nil when any level is missing.
func DigPath(data map[string]any, path string) any {
	var current any = data
	for _, token := range strings.Split(strings.TrimSpace(path), ".") {
		token = strings.TrimSpace(token)
		if token == "" {
			return nil
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

// ---------- Spec construction ----------

// CustomProviderAuth lists the accepted auth_mode spellings.
const (
	CustomAuthBearer = "bearer"
	CustomAuthQuery  = "query"
	CustomAuthHeader = "header"
	CustomAuthNone   = "none"
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
	default:
		problems = append(problems, "auth_mode: one of bearer, query, header, none")
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
	}
	switch p.AuthMode {
	case CustomAuthBearer:
		spec.Auth = AuthBearer
	case CustomAuthQuery:
		spec.Auth = AuthQuery
		spec.AuthParam = strings.TrimSpace(p.AuthParam)
	case CustomAuthHeader:
		spec.Auth = AuthHeader
		spec.AuthParam = strings.TrimSpace(p.AuthParam)
	case CustomAuthNone:
		// key never leaves the project row
	}
	spec.Check = customCheck(p)
	spec.Extract = func(data map[string]any) (float64, error) {
		value, ok := Num(DigPath(data, p.BalancePath))
		if !ok {
			return 0, fmt.Errorf("balance not found at %s", p.BalancePath)
		}
		return round2(value * scale), nil
	}
	return spec, nil
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
