// Command legacy-import turns a pre-1.0 QuotaPulse install into a bundle
// that 1.0 can import.
//
// It reads the old database and the old .env — nothing is written to either —
// and prints what it could carry over, what changed shape on the way, and
// what 1.0 has no place for.
//
//	go run . --env-file /path/to/.env -o bundle.json
//	quotapulse import --settings bundle.json
//
// The bundle contains every credential in the clear. Keep it where the .env
// was, and delete it once it has been imported.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "legacy-import:", err)
		os.Exit(1)
	}
}

func run() error {
	envFile := flag.String("env-file", "", "the old install's .env file")
	databaseURL := flag.String("database-url", "", "the old DATABASE_URL (default: taken from the .env)")
	lang := flag.String("lang", "", "language for names and notifications, en or zh (default: zh for a Chinese time zone)")
	output := flag.String("o", "", "write the bundle to this file (default: standard output)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: legacy-import --env-file FILE [-o bundle.json]\n\nReads a pre-1.0 QuotaPulse database and .env and writes a bundle for `quotapulse import`.")
		flag.PrintDefaults()
	}
	flag.Parse()

	env := map[string]string{}
	if *envFile != "" {
		loaded, err := loadEnvFile(*envFile)
		if err != nil {
			return err
		}
		env = loaded
	} else {
		// No file: the old settings are expected in the environment itself.
		for _, entry := range os.Environ() {
			if key, value, ok := strings.Cut(entry, "="); ok {
				env[key] = value
			}
		}
	}

	src := source{env: env, lang: *lang, now: time.Now()}
	target := *databaseURL
	if target == "" && isOn(env["ENABLE_DATABASE"]) {
		target = env["DATABASE_URL"]
		if target == "" {
			target = "sqlite:///./data/quotapulse.db"
		}
	}
	if target != "" {
		db, engine, err := openDatabase(target)
		if err != nil {
			return err
		}
		defer db.Close()
		if err := db.Ping(); err != nil {
			return fmt.Errorf("the old %s database is not reachable: %w", engine, err)
		}
		src.db = db
	}

	out, err := convert(src)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if *output == "" {
		_, err = os.Stdout.Write(raw)
	} else {
		err = os.WriteFile(*output, raw, 0o600)
	}
	if err != nil {
		return err
	}

	readings := 0
	for _, m := range out.Meters {
		readings += len(m.Readings)
	}
	fmt.Fprintf(os.Stderr, "Converted: %d account(s), %d meter(s) with %d reading(s), %d date(s), %d channel(s), %d token(s), %d custom provider(s), %d past incident(s).\n",
		len(out.Accounts), len(out.Meters), readings, len(out.Reminders), len(out.Channels), len(out.Tokens), len(out.Providers), len(out.Incidents))
	for _, note := range out.Notes {
		fmt.Fprintln(os.Stderr, "  •", note)
	}
	if *output != "" {
		fmt.Fprintf(os.Stderr, "\nThe bundle is in %s. It holds credentials in the clear: delete it after importing.\n", *output)
	}
	return nil
}

func isOn(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
