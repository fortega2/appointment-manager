package main

import (
	"appointment-manager/internal/appointment"
	"appointment-manager/internal/metrics"
	"appointment-manager/internal/notification"
	"context"
	"fmt"
	"log/slog"
	"uuid"
)

func initializeNotificationService(
	logger *slog.Logger,
	deps *dependencies,
	appMetrics *metrics.Metrics,
) (*notification.Service, error) {
	service, err := notification.NewService(logger, resolveSlotCancellation(deps.appointmentQuery), appMetrics)
	if err != nil {
		return nil, fmt.Errorf("failed to create notification service: %w", err)
	}

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
