package main

import (
	"appointment-manager/internal/appointment"
	"appointment-manager/internal/prescription"
	"appointment-manager/internal/storage"
	"appointment-manager/internal/ui/home"
	"appointment-manager/internal/ui/language"
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

func initializeUILanguageHandler(logger *slog.Logger, isDev bool) (*language.Handler, error) {
	languageHandler, err := language.NewHandler(logger, isDev)
	if err != nil {
		return nil, fmt.Errorf("failed to create language handler: %w", err)
	}

	return languageHandler, nil
}

func initializeUIAppointmentHandler(logger *slog.Logger, deps *dependencies) (*appointment.UIHandler, error) {
	appointmentHandler, err := appointment.NewUIHandler(
		logger,
		deps.appointmentService,
		deps.appointmentQuery,
		deps.prescriptionQuery,
		deps.professionalRepo,
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
