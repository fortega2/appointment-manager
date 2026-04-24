package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	defaultReadinessURL     = "http://127.0.0.1:8080/readyz"
	defaultRequestTimeout   = 2 * time.Second
	healthcheckExitSuccess  = 0
	healthcheckExitFailure  = 1
	healthcheckExitBadUsage = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	if stderr == nil {
		stderr = io.Discard
	}

	url, timeout, err := parseFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "healthcheck usage error: %v\n", err)
		return healthcheckExitBadUsage
	}

	if err := checkReady(url, timeout); err != nil {
		fmt.Fprintf(stderr, "healthcheck failed: %v\n", err)
		return healthcheckExitFailure
	}

	return healthcheckExitSuccess
}

func parseFlags(args []string) (string, time.Duration, error) {
	flagSet := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)

	url := flagSet.String("url", defaultReadinessURL, "readiness endpoint URL")
	timeout := flagSet.Duration("timeout", defaultRequestTimeout, "request timeout")

	if err := flagSet.Parse(args); err != nil {
		return "", 0, err
	}
	if *timeout <= 0 {
		return "", 0, errors.New("timeout must be greater than zero")
	}

	return *url, *timeout, nil
}

func checkReady(url string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	//nolint:gosec // Healthcheck URL is intentionally operator-configurable for local readiness probing.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	client := &http.Client{}
	//nolint:gosec // Healthcheck URL is intentionally operator-configurable for local readiness probing.
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}
