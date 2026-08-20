//go:build integration

package mailer_test

import (
	"appointment-manager/internal/mailer"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	mailpitImage    = "axllent/mailpit:v1.30.7"
	mailpitSMTPPort = "1025/tcp"
	mailpitHTTPPort = "8025/tcp"

	integrationFrom     = "noreply@example.com"
	integrationFromName = "Turnos"
	integrationTo       = "assistant@example.com"
	integrationSubject  = "Reset your password"
	integrationText     = "Open the link to choose a new password."
	integrationHTML     = "<p>Open the link to choose a new password.</p>"

	apiTimeout = 10 * time.Second
)

type mailpitRecipient struct {
	Address string `json:"Address"`
}

type mailpitSummary struct {
	ID      string             `json:"ID"`
	Subject string             `json:"Subject"`
	To      []mailpitRecipient `json:"To"`
	From    mailpitRecipient   `json:"From"`
}

type mailpitListing struct {
	Messages []mailpitSummary `json:"messages"`
	Total    int              `json:"total"`
}

type mailpitMessage struct {
	Text string `json:"Text"`
	HTML string `json:"HTML"`
}

// mailpitRelay starts a Mailpit container and reports the SMTP host and port to
// send through plus the base URL of its HTTP API to assert against.
func mailpitRelay(ctx context.Context, t *testing.T) (host string, smtpPort int, apiBaseURL string) {
	t.Helper()

	container, err := testcontainers.Run(ctx,
		mailpitImage,
		testcontainers.WithExposedPorts(mailpitSMTPPort, mailpitHTTPPort),
		testcontainers.WithWaitStrategy(wait.ForListeningPort(mailpitSMTPPort)),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, testcontainers.TerminateContainer(container))
	})

	host, err = container.Host(ctx)
	require.NoError(t, err)

	mappedSMTP, err := container.MappedPort(ctx, mailpitSMTPPort)
	require.NoError(t, err)

	mappedHTTP, err := container.MappedPort(ctx, mailpitHTTPPort)
	require.NoError(t, err)

	return host, int(mappedSMTP.Num()), fmt.Sprintf("http://%s:%d", host, mappedHTTP.Num())
}

func newIntegrationClient(t *testing.T, host string, port int) *mailer.Client {
	t.Helper()

	client, err := mailer.NewClient(mailer.Config{
		Host:        host,
		Port:        port,
		FromAddress: integrationFrom,
		FromName:    integrationFromName,
		UseTLS:      false,
	})
	require.NoError(t, err)

	return client
}

func getJSON(ctx context.Context, t *testing.T, url string, dst any) {
	t.Helper()

	reqCtx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, json.NewDecoder(resp.Body).Decode(dst))
}

func TestVerifyConnectionReachesTheRelay(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	host, smtpPort, _ := mailpitRelay(ctx, t)

	client := newIntegrationClient(t, host, smtpPort)
	require.NoError(t, client.VerifyConnection(ctx))
}

// TestVerifyConnectionReportsAnUnreachableRelay pins what the startup path
// depends on: a bad relay yields a client, and only the check fails.
func TestVerifyConnectionReportsAnUnreachableRelay(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	closedPort := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())

	client, err := mailer.NewClient(mailer.Config{
		Host:        "127.0.0.1",
		Port:        closedPort,
		FromAddress: integrationFrom,
		UseTLS:      false,
	})
	require.NoError(t, err)
	require.NotNil(t, client)

	assert.Error(t, client.VerifyConnection(t.Context()))
}

func TestSendDeliversTextAndHTML(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	host, smtpPort, apiBaseURL := mailpitRelay(ctx, t)
	client := newIntegrationClient(t, host, smtpPort)

	require.NoError(t, client.Send(ctx, mailer.Message{
		To:       integrationTo,
		Subject:  integrationSubject,
		TextBody: integrationText,
		HTMLBody: integrationHTML,
	}))

	var listing mailpitListing
	getJSON(ctx, t, apiBaseURL+"/api/v1/messages", &listing)

	require.Equal(t, 1, listing.Total)
	require.Len(t, listing.Messages, 1)

	summary := listing.Messages[0]
	assert.Equal(t, integrationSubject, summary.Subject)
	assert.Equal(t, integrationFrom, summary.From.Address)
	require.Len(t, summary.To, 1)
	assert.Equal(t, integrationTo, summary.To[0].Address)

	var delivered mailpitMessage
	getJSON(ctx, t, apiBaseURL+"/api/v1/message/"+summary.ID, &delivered)

	assert.Contains(t, delivered.Text, integrationText)
	assert.Contains(t, delivered.HTML, integrationHTML)
}

// TestSendWithoutHTMLLeavesNoAlternative pins that a text-only mail does not go
// out as an empty multipart.
func TestSendWithoutHTMLLeavesNoAlternative(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	host, smtpPort, apiBaseURL := mailpitRelay(ctx, t)
	client := newIntegrationClient(t, host, smtpPort)

	require.NoError(t, client.Send(ctx, mailer.Message{
		To:       integrationTo,
		Subject:  integrationSubject,
		TextBody: integrationText,
	}))

	var listing mailpitListing
	getJSON(ctx, t, apiBaseURL+"/api/v1/messages", &listing)
	require.Len(t, listing.Messages, 1)

	var delivered mailpitMessage
	getJSON(ctx, t, apiBaseURL+"/api/v1/message/"+listing.Messages[0].ID, &delivered)

	assert.Contains(t, delivered.Text, integrationText)
	assert.Empty(t, delivered.HTML)
}

// TestSendRejectsHeaderInjectionBeforeTheWire is the end-to-end counterpart of
// the unit test: the relay never sees the second header because nothing is sent.
func TestSendRejectsHeaderInjectionBeforeTheWire(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	host, smtpPort, apiBaseURL := mailpitRelay(ctx, t)
	client := newIntegrationClient(t, host, smtpPort)

	err := client.Send(ctx, mailer.Message{
		To:       integrationTo,
		Subject:  integrationSubject + "\r\nBcc: attacker@evil.com",
		TextBody: integrationText,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, mailer.ErrHeaderLineBreak)

	var listing mailpitListing
	getJSON(ctx, t, apiBaseURL+"/api/v1/messages", &listing)
	assert.Zero(t, listing.Total)
}

// TestSendTwiceReusesTheClient covers the lifecycle Send owns: it dials and
// closes per message, so a second send must work after the first is gone.
func TestSendTwiceReusesTheClient(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	host, smtpPort, apiBaseURL := mailpitRelay(ctx, t)
	client := newIntegrationClient(t, host, smtpPort)

	msg := mailer.Message{
		To:       integrationTo,
		Subject:  integrationSubject,
		TextBody: integrationText,
	}
	require.NoError(t, client.Send(ctx, msg))
	require.NoError(t, client.Send(ctx, msg))

	var listing mailpitListing
	getJSON(ctx, t, apiBaseURL+"/api/v1/messages", &listing)
	assert.Equal(t, 2, listing.Total)
}
