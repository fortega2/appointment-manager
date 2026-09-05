package notification_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"appointment-manager/internal/notification"
)

const (
	sentMessage           = "slot cancellation notification sent"
	noRecipientsMessage   = "cancelled slot had no recipients to notify"
	lookupFailedMessage   = "failed to resolve slot cancellation recipients"
	recipientDebugMessage = "slot cancellation recipient"

	lookupBoomError = "boom"

	kindSlotCancelled = "slot_cancelled"

	outcomeSent         = "sent"
	outcomeNoRecipients = "no_recipients"
	outcomeLookupFailed = "lookup_failed"
)

// Contact details that must never reach the log backend.
const (
	recipientName  = "Bruno Ferreyra"
	recipientEmail = "bruno.ferreyra@example.com"
	recipientPhone = "+54 11 5555 0100"
)

func oneRecipient(id string) notification.SlotCancellation {
	email := recipientEmail

	return notification.SlotCancellation{
		StartTime:            time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC),
		EndTime:              time.Date(2026, 6, 5, 10, 30, 0, 0, time.UTC),
		ProfessionalFullName: "Dr. Ruiz",
		Recipients: []notification.Recipient{
			{ID: id, FullName: recipientName, Email: &email, Phone: recipientPhone},
		},
	}
}

// noRecipients is the quiet default for tests that do not care who was affected.
func noRecipients(_ context.Context, _ uuid.UUID) (notification.SlotCancellation, error) {
	return notification.SlotCancellation{}, nil
}

// withRecipient resolves every slot to a single affected patient.
func withRecipient(_ context.Context, _ uuid.UUID) (notification.SlotCancellation, error) {
	return oneRecipient(uuid.NewV7().String()), nil
}

// failingLookup stands in for an unreachable database.
func failingLookup(_ context.Context, _ uuid.UUID) (notification.SlotCancellation, error) {
	return notification.SlotCancellation{}, errors.New(lookupBoomError)
}

func hasMessage(logs, msg string) bool {
	return strings.Contains(logs, `"msg":"`+msg+`"`)
}

// processedKey mirrors the label pair on appt_notifications_processed_total.
type processedKey struct {
	kind    string
	outcome string
}

type metricsSpy struct {
	processed map[processedKey]int
	observed  []string
}

func newMetricsSpy() *metricsSpy {
	return &metricsSpy{processed: make(map[processedKey]int)}
}

func (m *metricsSpy) RecordNotificationSent(kind string) {
	m.processed[processedKey{kind: kind, outcome: outcomeSent}]++
}

func (m *metricsSpy) RecordNotificationNoRecipients(kind string) {
	m.processed[processedKey{kind: kind, outcome: outcomeNoRecipients}]++
}

func (m *metricsSpy) RecordNotificationLookupFailed(kind string) {
	m.processed[processedKey{kind: kind, outcome: outcomeLookupFailed}]++
}

func (m *metricsSpy) ObserveNotificationSend(_ context.Context, kind string, _ time.Duration) {
	m.observed = append(m.observed, kind)
}

// totalProcessed sums every outcome, so a test can assert one call produced
// exactly one.
func (m *metricsSpy) totalProcessed() int {
	total := 0
	for _, count := range m.processed {
		total += count
	}

	return total
}

func newRecordingService(
	t *testing.T,
	lookup func(context.Context, uuid.UUID) (notification.SlotCancellation, error),
) (*notification.Service, *bytes.Buffer) {
	t.Helper()

	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	svc, err := notification.NewService(logger, lookup, nil)
	require.NoError(t, err)

	return svc, buf
}

func newMeteredService(
	t *testing.T,
	lookup func(context.Context, uuid.UUID) (notification.SlotCancellation, error),
) (*notification.Service, *metricsSpy) {
	t.Helper()

	spy := newMetricsSpy()
	svc, err := notification.NewService(slog.New(slog.DiscardHandler), lookup, spy)
	require.NoError(t, err)

	return svc, spy
}

func TestNewServiceValidation(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	tests := []struct {
		name     string
		logger   *slog.Logger
		lookup   func(context.Context, uuid.UUID) (notification.SlotCancellation, error)
		wantErrs []error
	}{
		{
			name:     "nil logger",
			lookup:   noRecipients,
			wantErrs: []error{notification.ErrNilLogger},
		},
		{
			name:     "nil recipient lookup",
			logger:   logger,
			wantErrs: []error{notification.ErrNilSlotCancellationFunc},
		},
		{
			name:     "every problem at once",
			wantErrs: []error{notification.ErrNilLogger, notification.ErrNilSlotCancellationFunc},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, err := notification.NewService(tt.logger, tt.lookup, nil)

			require.Error(t, err)
			assert.Nil(t, svc)
			for _, wantErr := range tt.wantErrs {
				assert.ErrorIs(t, err, wantErr)
			}
		})
	}
}

// The log backend has a different audience and retention from the database, so
// recipients are logged by opaque id only.
func TestSendSlotCancelledKeepsContactDetailsOutOfLogs(t *testing.T) {
	t.Parallel()

	patientID := uuid.NewV7()
	lookup := func(_ context.Context, _ uuid.UUID) (notification.SlotCancellation, error) {
		return oneRecipient(patientID.String()), nil
	}

	svc, buf := newRecordingService(t, lookup)

	require.NoError(t, svc.SendSlotCancelled(context.Background(), uuid.NewV7(), nil))

	logs := buf.String()

	assert.True(t, hasMessage(logs, sentMessage))
	assert.True(t, hasMessage(logs, recipientDebugMessage))
	assert.Contains(t, logs, patientID.String())
	assert.Contains(t, logs, `"recipients":1`)

	assert.NotContains(t, logs, recipientName)
	assert.NotContains(t, logs, recipientEmail)
	assert.NotContains(t, logs, recipientPhone)
}

// A slot nobody booked is an ordinary outcome, not a failure.
func TestSendSlotCancelledWithoutRecipientsIsNotAnError(t *testing.T) {
	t.Parallel()

	svc, buf := newRecordingService(t, noRecipients)

	require.NoError(t, svc.SendSlotCancelled(context.Background(), uuid.NewV7(), nil))

	logs := buf.String()
	assert.True(t, hasMessage(logs, noRecipientsMessage))
	assert.False(t, hasMessage(logs, lookupFailedMessage), "an empty result is not a failure")
	assert.False(t, hasMessage(logs, sentMessage), "nothing was delivered")
	assert.False(t, hasMessage(logs, recipientDebugMessage))
}

// A failed lookup is reported to the caller, so the outbox relay retries it.
func TestSendSlotCancelledStopsWhenLookupFails(t *testing.T) {
	t.Parallel()

	svc, buf := newRecordingService(t, failingLookup)

	err := svc.SendSlotCancelled(context.Background(), uuid.NewV7(), nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), lookupBoomError)

	logs := buf.String()
	assert.True(t, hasMessage(logs, lookupFailedMessage))
	assert.Contains(t, logs, lookupBoomError)
	assert.False(t, hasMessage(logs, sentMessage), "a failed lookup must not report a delivery")
	assert.False(t, hasMessage(logs, recipientDebugMessage))
}

func TestServiceRecordsSendOutcomes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		lookup  func(context.Context, uuid.UUID) (notification.SlotCancellation, error)
		name    string
		outcome string
	}{
		{name: "delivered", lookup: withRecipient, outcome: outcomeSent},
		{name: "nobody to notify", lookup: noRecipients, outcome: outcomeNoRecipients},
		{name: "recipients could not be resolved", lookup: failingLookup, outcome: outcomeLookupFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, spy := newMeteredService(t, tt.lookup)

			_ = svc.SendSlotCancelled(context.Background(), uuid.NewV7(), nil)

			assert.Equal(t, 1, spy.totalProcessed())
			assert.Equal(t, 1, spy.processed[processedKey{kind: kindSlotCancelled, outcome: tt.outcome}])
			assert.Equal(t, []string{kindSlotCancelled}, spy.observed)
		})
	}
}
