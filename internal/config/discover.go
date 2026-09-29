package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/provider"
)

// Implementation note.
const MaxEnvAccounts = 10

// Implementation note.
// Implementation note.
func LoadEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("%s line %d is not in KEY=VALUE format", path, lineNo)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		// Implementation note.
		if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' ||
			value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// Implementation note.
// Implementation note.
func envVariants(prefix, suffix string, ordinal int) []string {
	if ordinal <= 1 {
		return []string{prefix + "_" + suffix, prefix + "_1_" + suffix}
	}
	return []string{fmt.Sprintf("%s_%d_%s", prefix, ordinal, suffix)}
}

// Implementation note.
func envFirst(prefix, suffix string, ordinal int) string {
	for _, name := range envVariants(prefix, suffix, ordinal) {
		if value, ok := raw(name); ok {
			return value
		}
	}
	return ""
}

func envFirstFloat(prefix, suffix string, ordinal int) float64 {
	value := envFirst(prefix, suffix, ordinal)
	if value == "" {
		return 0
	}
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return f
}

// Implementation note.
func ProviderKeyEnvNames(providerKey string, ordinal int) []string {
	if providerKey == "" {
		return nil
	}
	return envVariants(strings.ToUpper(providerKey), "API_KEY", ordinal)
}

// Implementation note.
//
// Implementation note.
// Implementation note.
func DiscoverProjects(declared []model.Project) []model.Project {
	declaredProviders := make(map[string]bool, len(declared))
	for _, p := range declared {
		declaredProviders[strings.ToLower(strings.TrimSpace(p.Provider))] = true
	}

	var discovered []model.Project
	for _, key := range provider.Keys() {
		if declaredProviders[key] {
			continue
		}
		upper := strings.ToUpper(key)

		type account struct {
			ordinal int
			apiKey  string
		}
		var accounts []account
		for ordinal := 1; ordinal <= MaxEnvAccounts; ordinal++ {
			if apiKey := envFirst(upper, "API_KEY", ordinal); apiKey != "" {
				accounts = append(accounts, account{ordinal, apiKey})
			}
		}

		for _, acct := range accounts {
			name := key
			if len(accounts) > 1 {
				name = fmt.Sprintf("%s-%d", key, acct.ordinal)
			}
			discovered = append(discovered, model.Project{
				Name:         name,
				Provider:     key,
				APIKey:       acct.apiKey,
				Threshold:    envFirstFloat(upper, "THRESHOLD", acct.ordinal),
				Type:         model.DefaultBalanceType(key),
				OwnerProject: model.OwnerProjectOf(envFirst(upper, "OWNER_PROJECT", acct.ordinal)),
				Enabled:      true,
				FromEnv:      true,
			})
		}
	}
	return discovered
}

// Implementation note.
//
// Implementation note.
// Implementation note.
func DiscoverMailboxes(declared []model.Mailbox) []model.Mailbox {
	declaredNames := make(map[string]bool, len(declared)*2)
	for _, m := range declared {
		declaredNames[m.Name] = true
		declaredNames[m.Username] = true
	}

	var discovered []model.Mailbox
	for ordinal := 1; ordinal <= MaxEnvAccounts; ordinal++ {
		host := envFirst("EMAIL", "HOST", ordinal)
		username := envFirst("EMAIL", "USERNAME", ordinal)
		password := envFirst("EMAIL", "PASSWORD", ordinal)
		if host == "" || username == "" || password == "" {
			continue
		}
		name := envFirst("EMAIL", "NAME", ordinal)
		if name == "" {
			name = username
		}
		if declaredNames[name] || declaredNames[username] {
			continue
		}

		port := 993
		if value := envFirst("EMAIL", "PORT", ordinal); value != "" {
			if n, err := strconv.Atoi(value); err == nil && n > 0 {
				port = n
			}
		}
		useSSL := true
		if value := envFirst("EMAIL", "USE_SSL", ordinal); value != "" {
			useSSL = truthy[strings.ToLower(value)]
		}

		discovered = append(discovered, model.Mailbox{
			Name: name, Host: host, Port: port, Username: username,
			Password: password, UseSSL: useSSL, Enabled: true, FromEnv: true,
		})
		// Implementation note.
		declaredNames[name] = true
		declaredNames[username] = true
	}
	return discovered
}

// Implementation note.
// Implementation note.
func ResolveAPIKey(p model.Project, ordinal int) (string, string) {
	if p.APIKey != "" {
		return p.APIKey, "the api_key field in the configuration"
	}
	for _, name := range ProviderKeyEnvNames(p.Provider, ordinal) {
		if value, ok := raw(name); ok {
			return value, "environment variable " + name
		}
	}
	return "", ""
}
