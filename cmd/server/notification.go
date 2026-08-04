package main

import (
	"appointment-manager/internal/appointment"
	"appointment-manager/internal/metrics"
	"appointment-manager/internal/notification"
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/google/uuid"
)

// initializeNotificationService builds the notification service from the
// NOTIFICATION_* env vars. It is separate from startNotificationWorker because
// the two happen at different points in start-up: the service must exist before
// the handlers are built, so the slot handler can bind NotifySlotCancelled, but
// its drain goroutine must not run until the process is otherwise ready.
func initializeNotificationService(
	logger *slog.Logger,
	deps *dependencies,
	appMetrics *metrics.Metrics,
) (*notification.Service, error) {
	tickerInterval, bufferSize, err := parseNotificationConfig(
		logger,
		os.Getenv(notificationTickerIntervalEnv),
		os.Getenv(notificationBufferSizeEnv),
	)
	if err != nil {
		return nil, err
	}

	service, err := notification.NewService(
		logger,
		tickerInterval,
		bufferSize,
		resolveSlotCancellation(deps.appointmentQuery),
		appMetrics,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create notification service: %w", err)
	}

	// Registered here rather than in metrics.New because the queue does not
	// exist until the service does. The gauges read the channel on scrape, so
	// they report the buffer as this process actually configured it.
	appMetrics.RegisterNotificationQueue(
		func() float64 { return float64(service.QueueDepth()) },
		func() float64 { return float64(service.QueueCapacity()) },
	)

	return service, nil
}

// resolveSlotCancellation adapts the appointment read model to the notification
// package's own view of a cancellation. The translation lives here, in the only
// place that knows about both packages: notification never imports appointment,
// so its recipients stay whatever a notification needs rather than whatever the
// appointment schema happens to hold.
func resolveSlotCancellation(query *appointment.Query) func(context.Context, uuid.UUID) (notification.SlotCancellation, error) {
	return func(ctx context.Context, slotID uuid.UUID) (notification.SlotCancellation, error) {
		cancellation, err := query.SlotCancellationRecipients(ctx, slotID)
		if err != nil {
			return notification.SlotCancellation{}, err
		}

		recipients := make([]notification.Recipient, 0, len(cancellation.Recipients))
		for _, recipient := range cancellation.Recipients {
			recipients = append(recipients, notification.Recipient{
				FullName: recipient.FullName,
				Phone:    recipient.Phone,
				ID:       recipient.PatientID,
				Email:    recipient.Email,
			})
		}

		return notification.SlotCancellation{
			StartTime:            cancellation.StartTime,
			EndTime:              cancellation.EndTime,
			Recipients:           recipients,
			ProfessionalFullName: cancellation.ProfessionalFullName,
		}, nil
	}
}

// startNotificationWorker runs the drain loop until the returned stop func is
// called. stop blocks until the goroutine has exited, which includes the final
// flush, so callers must defer it while the pool is still open -- the flush
// queries the database for recipients.
//
// ctx must not be the shutdown-signal context: cancelling it is what ends the
// drain, and the drain has to outlive the HTTP server's graceful shutdown so
// requests still finishing can queue a notification that someone will send.
// Only the returned stop func should end it.
func startNotificationWorker(ctx context.Context, service *notification.Service) func() {
	workerCtx, cancelWorker := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		service.Run(workerCtx)
	}()

	return func() {
		cancelWorker()
		<-workerDone
	}
}
