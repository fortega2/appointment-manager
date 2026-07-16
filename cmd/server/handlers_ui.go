package main

import (
	"appointment-manager/internal/appointment"
	"appointment-manager/internal/prescription"
	"appointment-manager/internal/storage"
	"appointment-manager/internal/ui/home"
	"fmt"
	"log/slog"
)

func initializeUIHomeHandler(logger *slog.Logger) (*home.Handler, error) {
	homeHandler, err := home.NewHandler(logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create home handler: %w", err)
	}

	return homeHandler, nil
}

func initializeUIAppointmentHandler(logger *slog.Logger, deps *dependencies) (*appointment.UIHandler, error) {
	appointmentHandler, err := appointment.NewUIHandler(
		logger,
		deps.appointmentService,
		deps.appointmentQuery,
		deps.prescriptionQuery,
		deps.professionalRepo,
		deps.assistantRepo,
		deps.slotQuery,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create appointment UI handler: %w", err)
	}

	return appointmentHandler, nil
}

func initializeUIPrescriptionHandler(logger *slog.Logger, deps *dependencies, storageClient *storage.Client) (*prescription.UIHandler, error) {
	svc, err := prescription.NewService(deps.prescriptionRepo, storageClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create prescription service: %w", err)
	}
	prescriptionHandler, err := prescription.NewUIHandler(logger, svc, deps.prescriptionQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to create prescription UI handler: %w", err)
	}

	return prescriptionHandler, nil
}
