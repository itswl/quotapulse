package provider

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/itchyny/gojq"

	"github.com/itswl/quotapulse/internal/core"
)

// prelude is prepended to every extract program.
//
// num reads a number wherever a platform hid it: a real number, a string
// ("547.67"), a string with thousands separators ("1,234.56"), or a sentence
// with a number in it ("剩余263419余额").
const prelude = `
def num:
  if type == "number" then .
  elif type == "string" then
    (gsub(","; "") | [match("-?[0-9]+(\\.[0-9]+)?").string] |
      if length == 0 then error("expected a number") else .[0] | tonumber end)
  else error("expected a number, got \(type)") end;
`

// A jq program can loop forever; give it a moment, not a minute.
const extractTimeout = 2 * time.Second

var (
	compiled   sync.Map // extract source -> *gojq.Code
	keyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
)

func compile(extract string) (*gojq.Code, error) {
	if cached, ok := compiled.Load(extract); ok {
		return cached.(*gojq.Code), nil
	}
	query, err := gojq.Parse(prelude + extract)
	if err != nil {
		return nil, fmt.Errorf("invalid jq: %w", err)
	}
	// No environment loader and no input iterator: a definition can read the
	// response it was given and nothing else on this machine.
	code, err := gojq.Compile(query, gojq.WithVariables([]string{"$now", "$fields"}))
	if err != nil {
		return nil, fmt.Errorf("invalid jq: %w", err)
	}
	compiled.Store(extract, code)
	return code, nil
}

// Sample is one meter read from a response.
type Sample struct {
	Key      string     `json:"key"`
	Name     string     `json:"name,omitempty"`
	NameZH   string     `json:"name_zh,omitempty"`
	Kind     core.Kind  `json:"kind"`
	Unit     string     `json:"unit"`
	Value    float64    `json:"value"`
	Limit    *float64   `json:"limit,omitempty"`
	ResetsAt *time.Time `json:"resets_at,omitempty"`
	Window   *int64     `json:"window_seconds,omitempty"`
}

// Extract runs a definition's jq program over a response body.
// fields are the account's non-secret fields, visible to the program as $fields.
func Extract(ctx context.Context, extract string, body any, fields map[string]string, now time.Time) ([]Sample, error) {
	code, err := compile(extract)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, extractTimeout)
	defer cancel()

	visible := make(map[string]any, len(fields))
	for k, v := range fields {
		visible[k] = v
	}
	var samples []Sample
	seen := map[string]bool{}
	iter := code.RunWithContext(ctx, body, float64(now.Unix()), visible)
	for {
		value, ok := iter.Next()
		if !ok {
			break
		}
		if err, isErr := value.(error); isErr {
			var halt *gojq.HaltError
			if errors.As(err, &halt) && halt.Value() == nil {
				break
			}
			return nil, errors.New(jqMessage(err))
		}
		items := []any{value}
		if list, isList := value.([]any); isList {
			items = list
		}
		for _, item := range items {
			if item == nil {
				continue
			}
			object, isObject := item.(map[string]any)
			if !isObject {
				return nil, fmt.Errorf("extract must output objects, got %s", jsonType(item))
			}
			sample, err := toSample(object)
			if err != nil {
				return nil, err
			}
			if seen[sample.Key] {
				return nil, fmt.Errorf("extract output two meters with the key %q", sample.Key)
			}
			seen[sample.Key] = true
			samples = append(samples, sample)
		}
	}
	if len(samples) == 0 {
		return nil, errors.New("the response held no meters")
	}
	return samples, nil
}

// jqMessage is what to show for a jq failure: the text handed to error("…")
// when the program raised it on purpose, jq's own message otherwise.
func jqMessage(err error) string {
	var raised gojq.ValueError
	if errors.As(err, &raised) {
		switch v := raised.Value().(type) {
		case string:
			return v
		case nil:
		default:
			return fmt.Sprint(v)
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "extract did not finish in time"
	}
	return strings.TrimPrefix(err.Error(), "gojq: ")
}

func toSample(o map[string]any) (Sample, error) {
	s := Sample{Key: "main", Kind: core.Balance}
	if raw, ok := o["key"]; ok && raw != nil {
		key, isString := raw.(string)
		if !isString || !keyPattern.MatchString(key) {
			return s, fmt.Errorf("meter key %v must be lowercase letters, digits, dots, dashes or underscores", raw)
		}
		s.Key = key
	}
	s.Name, _ = o["name"].(string)
	s.NameZH, _ = o["name_zh"].(string)
	s.Unit, _ = o["unit"].(string)
	if raw, ok := o["kind"]; ok && raw != nil {
		kind, _ := raw.(string)
		if s.Kind = core.Kind(kind); !s.Kind.Valid() {
			return s, fmt.Errorf("meter %q: kind must be balance, quota or counter, not %v", s.Key, raw)
		}
	}

	value, ok := number(o["value"])
	if !ok {
		return s, fmt.Errorf("meter %q: value must be a number, got %s", s.Key, jsonType(o["value"]))
	}
	s.Value = tidy(value)
	if raw := o["limit"]; raw != nil {
		limit, ok := number(raw)
		if !ok || limit < 0 {
			return s, fmt.Errorf("meter %q: limit must be a number", s.Key)
		}
		limit = tidy(limit)
		s.Limit = &limit
	}
	if raw := o["window"]; raw != nil {
		seconds, ok := number(raw)
		if !ok || seconds <= 0 {
			return s, fmt.Errorf("meter %q: window must be a number of seconds", s.Key)
		}
		s.Window = core.Ptr(int64(seconds))
	}
	if raw := o["resets_at"]; raw != nil {
		at, err := instant(raw)
		if err != nil {
			return s, fmt.Errorf("meter %q: resets_at %v", s.Key, err)
		}
		s.ResetsAt = at
	}
	return s, nil
}

// tidy rounds away binary floating-point noise. "150 - 131.6" is
// 18.400000000000006 to a computer and 18.4 to everyone else; stored as it
// comes, every such reading would also look like a change from the last one.
func tidy(v float64) float64 { return math.Round(v*1e6) / 1e6 }

// number accepts a JSON number or a numeric string. NaN and infinities are
// not numbers: they would poison every sum downstream.
func number(v any) (float64, bool) {
	var f float64
	switch n := v.(type) {
	case float64:
		f = n
	case int:
		f = float64(n)
	case *big.Int:
		f, _ = new(big.Float).SetInt(n).Float64()
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err != nil {
			return 0, false
		}
		f = parsed
	default:
		return 0, false
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// instant reads a point in time: Unix seconds, Unix milliseconds, or RFC 3339.
// Zero and negative values mean "none".
func instant(v any) (*time.Time, error) {
	if text, ok := v.(string); ok {
		if at, err := time.Parse(time.RFC3339, strings.TrimSpace(text)); err == nil {
			return &at, nil
		}
	}
	seconds, ok := number(v)
	if !ok {
		return nil, errors.New("must be a Unix time or an RFC 3339 string")
	}
	if seconds <= 0 {
		return nil, nil
	}
	if seconds > 1e11 { // milliseconds
		seconds /= 1000
	}
	at := time.Unix(int64(seconds), 0).UTC()
	return &at, nil
}

func jsonType(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "a boolean"
	case string:
		return "a string"
	case float64, int, *big.Int:
		return "a number"
	case []any:
		return "an array"
	case map[string]any:
		return "an object"
	}
	return fmt.Sprintf("%T", v)
}
