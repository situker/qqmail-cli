package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/situker/qqmailctl/internal/account"
	"github.com/situker/qqmailctl/internal/output"
	"github.com/situker/qqmailctl/internal/secrets"
)

type options struct {
	spike     string
	output    string
	config    string
	account   string
	label     string
	duration  time.Duration
	interval  time.Duration
	maxLogins int
	write     bool
}

type result struct {
	Schema       int            `json:"schema"`
	Spike        string         `json:"spike"`
	ObservedAt   time.Time      `json:"observed_at"`
	Status       string         `json:"status"`
	Observations map[string]any `json:"observations"`
	Pending      []string       `json:"pending"`
	Notes        []string       `json:"notes"`
}

func main() {
	os.Exit(run())
}

func run() (exitCode int) {
	defer func() {
		if recovered := recover(); recovered != nil {
			_, _ = fmt.Fprintln(os.Stderr, output.RedactString(fmt.Sprintf("probe panic: %v", recovered)))
			exitCode = 1
		}
	}()
	var opts options
	flag.StringVar(&opts.spike, "spike", "", "spike identifier S1 through S11")
	flag.StringVar(&opts.output, "output", "", "optional JSON result path")
	flag.StringVar(&opts.config, "config", "", "optional qqmailctl config path")
	flag.StringVar(&opts.account, "account", "", "configured account name")
	flag.StringVar(&opts.label, "label", "current", "snapshot label")
	flag.DurationVar(&opts.duration, "duration", 30*time.Second, "observation duration")
	flag.DurationVar(&opts.interval, "interval", 15*time.Second, "interval between risky login probes")
	flag.IntVar(&opts.maxLogins, "max-logins", 3, "maximum S4 login attempts")
	flag.BoolVar(&opts.write, "write", false, "request the gated write/rate probe phase")
	flag.Parse()

	opts.spike = strings.ToUpper(strings.TrimSpace(opts.spike))
	if !validSpike(opts.spike) {
		_, _ = fmt.Fprintln(os.Stderr, "-spike must be S1 through S11 or READONLY")
		return 2
	}
	probeResult := result{Schema: 1, Spike: opts.spike, ObservedAt: time.Now().UTC(), Status: "completed", Observations: map[string]any{}, Pending: []string{}, Notes: []string{"Provider behavior is an observation for this account and date, not a guarantee."}}

	var named account.Named
	var authCode string
	if needsIMAPAccount(opts.spike) {
		var err error
		named, authCode, err = loadAccount(opts)
		if err != nil {
			return fail(err)
		}
	}

	var err error
	switch opts.spike {
	case "S1":
		err = probeS1(named, authCode, &probeResult)
	case "S2":
		err = probeS2(named, authCode, &probeResult)
	case "S3":
		err = probeS3(named, authCode, &probeResult)
	case "S4":
		err = probeS4(named, authCode, opts, &probeResult)
	case "S5":
		err = probeS5(named, authCode, opts, &probeResult)
	case "S6":
		err = probeS6(named, authCode, opts.duration, &probeResult)
	case "S7":
		err = probeS7(named, authCode, opts.label, &probeResult)
	case "S8":
		err = probeS8(named, authCode, &probeResult)
	case "S9":
		probeResult.Status = "pending_manual_trigger"
		probeResult.Pending = append(probeResult.Pending, "Run spikes/s09-smtp-quota.ps1 after the v0.3 send command exists; it requires both write safety gates and interactive confirmation.")
	case "S10":
		err = probeS10(&probeResult)
	case "S11":
		err = probeS11(named, authCode, opts, &probeResult)
	case "READONLY":
		err = probeReadonlySuite(named, authCode, opts.duration, &probeResult)
	}
	if err != nil {
		return fail(err)
	}
	if err := emit(probeResult, opts.output); err != nil {
		return fail(err)
	}
	return 0
}

func validSpike(value string) bool {
	if value == "READONLY" {
		return true
	}
	for i := 1; i <= 11; i++ {
		if value == fmt.Sprintf("S%d", i) {
			return true
		}
	}
	return false
}

func needsIMAPAccount(spike string) bool {
	return spike != "S9" && spike != "S10"
}

func loadAccount(opts options) (account.Named, string, error) {
	cfg, _, err := account.Load(opts.config)
	if err != nil {
		return account.Named{}, "", err
	}
	named, err := cfg.Resolve(opts.account)
	if err != nil {
		return account.Named{}, "", err
	}
	authCode, err := (secrets.Keyring{}).Get(named.Email)
	if err != nil {
		return account.Named{}, "", err
	}
	return named, authCode, nil
}

func emit(value result, path string) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			return err
		}
	}
	_, err = os.Stdout.Write(raw)
	return err
}

func fail(err error) int {
	_, _ = fmt.Fprintln(os.Stderr, output.RedactString(err.Error()))
	return 1
}
