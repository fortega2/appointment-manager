package main

import (
	"appointment-manager/internal/appointment"
	"appointment-manager/internal/assistant"
	"appointment-manager/internal/auth"
	"appointment-manager/internal/health"
	"appointment-manager/internal/healthinsurance"
	"appointment-manager/internal/password"
	"appointment-manager/internal/patient"
	"appointment-manager/internal/professional"
	"appointment-manager/internal/session"
	"appointment-manager/internal/slot"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const readinessTimeout = 300 * time.Millisecond

func initializeAuthHandler(logger *slog.Logger, store *session.Store, pool *pgxpool.Pool, pass *password.Argon2, isDev bool) (*auth.Handler, error) {
	authRepo, err := assistant.NewPostgresRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth postgres repository: %w", err)
	}
	authHandler, err := auth.NewHandler(logger, store, authRepo, pass, isDev)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth handler: %w", err)
	}

	return authHandler, nil
}

func initializeAssistantHandler(logger *slog.Logger, pool *pgxpool.Pool) (*assistant.Handler, error) {
	assistantRepo, err := assistant.NewPostgresRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create assistant postgres repository: %w", err)
	}
	passwordHasher := password.NewArgon2()
	assistantService, err := assistant.NewService(assistantRepo, passwordHasher)
	if err != nil {
		return nil, fmt.Errorf("failed to create assistant service: %w", err)
	}
	assistantHandler, err := assistant.NewHandler(logger, assistantService)
	if err != nil {
		return nil, fmt.Errorf("failed to create assistant handler: %w", err)
	}

	return assistantHandler, nil
}

func initializeAppointmentHandler(logger *slog.Logger, pool *pgxpool.Pool) (*appointment.Handler, error) {
	appointmentRepo, err := appointment.NewPostgresRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create appointment postgres repository: %w", err)
	}
	appointmentService, err := appointment.NewService(appointmentRepo)
	if err != nil {
		return nil, fmt.Errorf("failed to create appointment service: %w", err)
	}
	appointmentHandler, err := appointment.NewHandler(logger, appointmentService)
	if err != nil {
		return nil, fmt.Errorf("failed to create appointment handler: %w", err)
	}

	return appointmentHandler, nil
}

func initializeProfessionalHandler(logger *slog.Logger, pool *pgxpool.Pool) (*professional.Handler, error) {
	professionalRepo, err := professional.NewRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create professional repository: %w", err)
	}
	professionalHandler, err := professional.NewHandler(logger, professionalRepo)
	if err != nil {
		return nil, fmt.Errorf("failed to create professional handler: %w", err)
	}

	return professionalHandler, nil
}

func initializePatientHandler(logger *slog.Logger, pool *pgxpool.Pool) (*patient.Handler, error) {
	patientRepo, err := patient.NewRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create patient repository: %w", err)
	}
	healthInsuranceRepo, err := healthinsurance.NewRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create health insurance repository: %w", err)
	}
	patientHandler, err := patient.NewHandler(logger, patientRepo, healthInsuranceRepo)
	if err != nil {
		return nil, fmt.Errorf("failed to create patient handler: %w", err)
	}

	return patientHandler, nil
}

func initializeSlotHandler(logger *slog.Logger, pool *pgxpool.Pool) (*slot.Handler, error) {
	repo, err := slot.NewRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create slot repository: %w", err)
	}
	query, err := slot.NewQuery(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create slot query: %w", err)
	}
	pRepo, err := professional.NewRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create professional repository for slot handler: %w", err)
	}

	h, err := slot.NewHandler(logger, repo, query, pRepo)
	if err != nil {
		return nil, fmt.Errorf("failed to create slot handler: %w", err)
	}

	return h, nil
}

func initializeHealthHandler(logger *slog.Logger, pool *pgxpool.Pool) (*health.Handler, error) {
	checkReady, err := health.NewPgxReadinessCheck(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create health readiness checker: %w", err)
	}
	handler, err := health.NewHandler(logger, checkReady, readinessTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create health handler: %w", err)
	}

	return handler, nil
}
