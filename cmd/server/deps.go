package main

import (
	"appointment-manager/internal/appointment"
	"appointment-manager/internal/assistant"
	"appointment-manager/internal/health"
	"appointment-manager/internal/healthinsurance"
	"appointment-manager/internal/metrics"
	"appointment-manager/internal/password"
	"appointment-manager/internal/patient"
	"appointment-manager/internal/prescription"
	"appointment-manager/internal/professional"
	"appointment-manager/internal/slot"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// dependencies holds the repositories, queries and services shared by the
// handlers and the worker. Every value is immutable once built and stateless
// over the pool, so one instance per type is enough: sharing them keeps the
// nil-pool check in a single place instead of in each handler constructor.
type dependencies struct {
	passwordHasher *password.Argon2
	readinessCheck health.CheckReady

	appointmentRepo    *appointment.PostgresRepository
	appointmentQuery   *appointment.Query
	appointmentService *appointment.Service

	assistantRepo *assistant.PostgresRepository

	healthInsuranceRepo *healthinsurance.Repository

	patientRepo *patient.Repository

	prescriptionRepo  *prescription.Repository
	prescriptionQuery *prescription.Query

	professionalRepo *professional.Repository

	slotRepo  *slot.Repository
	slotQuery *slot.Query
}

// newDependencies builds every shared collaborator from the pool. Each
// constructor here fails only when the pool is nil, so an error means the
// process cannot serve anything and must not start.
func newDependencies(pool *pgxpool.Pool, appointmentMetrics *metrics.Metrics) (*dependencies, error) {
	appointmentRepo, err := appointment.NewPostgresRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create appointment postgres repository: %w", err)
	}
	appointmentQuery, err := appointment.NewQuery(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create appointment query: %w", err)
	}
	appointmentService, err := appointment.NewService(appointmentRepo, appointmentMetrics)
	if err != nil {
		return nil, fmt.Errorf("failed to create appointment service: %w", err)
	}

	assistantRepo, err := assistant.NewPostgresRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create assistant postgres repository: %w", err)
	}

	healthInsuranceRepo, err := healthinsurance.NewRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create health insurance repository: %w", err)
	}

	patientRepo, err := patient.NewRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create patient repository: %w", err)
	}

	prescriptionRepo, err := prescription.NewRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create prescription repository: %w", err)
	}
	prescriptionQuery, err := prescription.NewQuery(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create prescription query: %w", err)
	}

	professionalRepo, err := professional.NewRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create professional repository: %w", err)
	}

	slotRepo, err := slot.NewRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create slot repository: %w", err)
	}
	slotQuery, err := slot.NewQuery(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create slot query: %w", err)
	}

	readinessCheck, err := health.NewPgxReadinessCheck(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create health readiness checker: %w", err)
	}

	return &dependencies{
		passwordHasher:      password.NewArgon2(),
		readinessCheck:      readinessCheck,
		appointmentRepo:     appointmentRepo,
		appointmentQuery:    appointmentQuery,
		appointmentService:  appointmentService,
		assistantRepo:       assistantRepo,
		healthInsuranceRepo: healthInsuranceRepo,
		patientRepo:         patientRepo,
		prescriptionRepo:    prescriptionRepo,
		prescriptionQuery:   prescriptionQuery,
		professionalRepo:    professionalRepo,
		slotRepo:            slotRepo,
		slotQuery:           slotQuery,
	}, nil
}
