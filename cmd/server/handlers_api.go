package main

import (
	"appointment-manager/internal/appointment"
	"appointment-manager/internal/assistant"
	"appointment-manager/internal/auth"
	"appointment-manager/internal/health"
	"appointment-manager/internal/patient"
	"appointment-manager/internal/professional"
	"appointment-manager/internal/session"
	"appointment-manager/internal/slot"
	"fmt"
	"log/slog"
	"time"
)

const readinessTimeout = 300 * time.Millisecond

func initializeAuthHandler(logger *slog.Logger, store *session.Store, deps *dependencies, isDev bool) (*auth.Handler, error) {
	authHandler, err := auth.NewHandler(logger, store, deps.assistantRepo, deps.passwordHasher, isDev)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth handler: %w", err)
	}

	return authHandler, nil
}

func initializeAssistantHandler(logger *slog.Logger, deps *dependencies) (*assistant.Handler, error) {
	assistantService, err := assistant.NewService(deps.assistantRepo, deps.passwordHasher)
	if err != nil {
		return nil, fmt.Errorf("failed to create assistant service: %w", err)
	}
	assistantHandler, err := assistant.NewHandler(logger, assistantService)
	if err != nil {
		return nil, fmt.Errorf("failed to create assistant handler: %w", err)
	}

	return assistantHandler, nil
}

func initializeAppointmentHandler(logger *slog.Logger, deps *dependencies) (*appointment.Handler, error) {
	appointmentHandler, err := appointment.NewHandler(logger, deps.appointmentService)
	if err != nil {
		return nil, fmt.Errorf("failed to create appointment handler: %w", err)
	}

	return appointmentHandler, nil
}

func initializeProfessionalHandler(logger *slog.Logger, deps *dependencies) (*professional.Handler, error) {
	professionalHandler, err := professional.NewHandler(logger, deps.professionalRepo)
	if err != nil {
		return nil, fmt.Errorf("failed to create professional handler: %w", err)
	}

	return professionalHandler, nil
}

func initializePatientHandler(logger *slog.Logger, deps *dependencies) (*patient.Handler, error) {
	patientHandler, err := patient.NewHandler(logger, deps.patientRepo, deps.healthInsuranceRepo)
	if err != nil {
		return nil, fmt.Errorf("failed to create patient handler: %w", err)
	}

	return patientHandler, nil
}

func initializeSlotHandler(logger *slog.Logger, deps *dependencies) (*slot.Handler, error) {
	slotHandler, err := slot.NewHandler(logger, deps.slotRepo, deps.slotQuery, deps.professionalRepo)
	if err != nil {
		return nil, fmt.Errorf("failed to create slot handler: %w", err)
	}

	return slotHandler, nil
}

func initializeHealthHandler(logger *slog.Logger, deps *dependencies) (*health.Handler, error) {
	healthHandler, err := health.NewHandler(logger, deps.readinessCheck, readinessTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create health handler: %w", err)
	}

	return healthHandler, nil
}
