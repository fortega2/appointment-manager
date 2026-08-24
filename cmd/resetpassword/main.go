// Command resetpassword resets an assistant's password from the command line,
// for when the mailed reset link is not an option: no relay configured, a relay
// that is down, or a mail that never arrives.
package main

import (
	"appointment-manager/internal/assistant"
	"appointment-manager/internal/password"
	"appointment-manager/internal/passwordreset"
	"appointment-manager/internal/session"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	databaseURLEnv = "DATABASE_URL"

	// defaultTimeout covers the whole run. It is generous because one Argon2id
	// hash costs seconds on the arm64 host this ships to, and a rescue command
	// that times out is not a rescue.
	defaultTimeout = 60 * time.Second

	// passwordBytes yields a 24-character password, well inside the bounds
	// password.Validate enforces.
	passwordBytes = 18

	exitSuccess  = 0
	exitFailure  = 1
	exitBadUsage = 2
)

var (
	errEmptyEmail         = errors.New("email is required")
	errNonPositiveTimeout = errors.New("timeout must be greater than zero")
	errMissingDatabaseURL = errors.New("DATABASE_URL is not set")
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	email, timeout, err := parseFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "resetpassword usage error: %v\n", err)
		return exitBadUsage
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	result, err := resetPassword(ctx, email)
	if err != nil {
		fmt.Fprintf(stderr, "resetpassword failed: %v\n", err)
		return exitFailure
	}

	// The password is the only thing on stdout, so it can be captured without
	// filtering diagnostics back out.
	fmt.Fprintln(stdout, result.password)
	fmt.Fprintf(stderr, "password reset for %s, %d session(s) closed, %d reset link(s) revoked\n",
		email, result.sessions, result.links)

	return exitSuccess
}

func parseFlags(args []string) (string, time.Duration, error) {
	flagSet := flag.NewFlagSet("resetpassword", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)

	email := flagSet.String("email", "", "email of the assistant to reset")
	timeout := flagSet.Duration("timeout", defaultTimeout, "budget for the whole run")

	if err := flagSet.Parse(args); err != nil {
		return "", 0, err
	}

	// Trimmed and nothing else: the lookup has to match the login's, and the
	// login does not fold case either. See ADR 0010.
	trimmed := strings.TrimSpace(*email)
	if trimmed == "" {
		return "", 0, errEmptyEmail
	}
	if *timeout <= 0 {
		return "", 0, errNonPositiveTimeout
	}

	return trimmed, *timeout, nil
}

// rescueResult is what the rescue changed.
type rescueResult struct {
	password string
	sessions int64
	links    int64
}

func resetPassword(ctx context.Context, email string) (rescueResult, error) {
	databaseURL := strings.TrimSpace(os.Getenv(databaseURLEnv))
	if databaseURL == "" {
		return rescueResult{}, errMissingDatabaseURL
	}

	// pgxpool directly rather than db.NewPostgresPool: that one applies the
	// migrations, and a rescue command must neither move the schema nor fail
	// because a half-applied migration left it dirty. See ADR 0010.
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return rescueResult{}, fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	// pgxpool.New connects lazily, so without this a bad host would surface as
	// a confusing failure inside the first query instead.
	if err := pool.Ping(ctx); err != nil {
		return rescueResult{}, fmt.Errorf("ping database: %w", err)
	}

	assistants, err := assistant.NewPostgresRepository(pool)
	if err != nil {
		return rescueResult{}, err
	}

	sessions, err := session.NewPostgresRepository(pool)
	if err != nil {
		return rescueResult{}, err
	}

	links, err := passwordreset.NewPostgresRepository(pool)
	if err != nil {
		return rescueResult{}, err
	}

	a, err := assistants.GetByEmail(ctx, email)
	if err != nil {
		return rescueResult{}, fmt.Errorf("look up assistant: %w", err)
	}

	plain, err := generatePassword()
	if err != nil {
		return rescueResult{}, err
	}

	// The login's own hasher: other Argon2 parameters would write a hash no
	// login could verify, locking the account out silently.
	hash, err := password.NewArgon2(nil).Hash(ctx, plain)
	if err != nil {
		return rescueResult{}, fmt.Errorf("hash password: %w", err)
	}

	// Sessions first: if the update then fails the account is merely logged
	// out, where the reverse order would leave a stolen session alive against a
	// password its owner no longer knows. See ADR 0010.
	closed, err := sessions.DeleteByAssistant(ctx, a.ID.String())
	if err != nil {
		return rescueResult{}, fmt.Errorf("close sessions: %w", err)
	}

	// This command runs when the mail path is not trusted, so a link already in
	// somebody's inbox must not outlive the rescue.
	revoked, err := links.DeleteByAssistant(ctx, a.ID)
	if err != nil {
		return rescueResult{}, fmt.Errorf("revoke reset links: %w", err)
	}

	if err := assistants.UpdatePasswordHash(ctx, a.ID, hash); err != nil {
		return rescueResult{}, fmt.Errorf("update password hash: %w", err)
	}

	return rescueResult{password: plain, sessions: closed, links: revoked}, nil
}

// generatePassword returns a fresh random password, checked against the same
// rule the reset form applies so the two can never disagree.
func generatePassword() (string, error) {
	buf := make([]byte, passwordBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}

	plain := base64.RawURLEncoding.EncodeToString(buf)
	if err := password.Validate(plain); err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}

	return plain, nil
}
