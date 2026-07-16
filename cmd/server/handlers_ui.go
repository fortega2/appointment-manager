package main

import (
	"appointment-manager/internal/appointment"
	"appointment-manager/internal/assistant"
	"appointment-manager/internal/prescription"
	"appointment-manager/internal/professional"
	"appointment-manager/internal/slot"
	"appointment-manager/internal/storage"
	"appointment-manager/internal/ui/home"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

func initializeUIHomeHandler(logger *slog.Logger) (*home.Handler, error) {
	homeHandler, err := home.NewHandler(logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create home handler: %w", err)
	}

	return homeHandler, nil
}

func initializeUIAppointmentHandler(logger *slog.Logger, pool *pgxpool.Pool) (*appointment.UIHandler, error) {
	appointmentRepo, err := appointment.NewPostgresRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create appointment postgres repository: %w", err)
	}
	appointmentQuery, err := appointment.NewQuery(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create appointment query: %w", err)
	}
	appointmentService, err := appointment.NewService(appointmentRepo)
	if err != nil {
		return nil, fmt.Errorf("failed to create appointment service: %w", err)
	}
	prescriptionQuery, err := prescription.NewQuery(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create prescription query for appointment UI: %w", err)
	}
	profRepo, err := professional.NewRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create professional repository for appointment UI: %w", err)
	}
	asstRepo, err := assistant.NewPostgresRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create assistant repository for appointment UI: %w", err)
	}
	slotQuery, err := slot.NewQuery(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create slot query for appointment UI: %w", err)
	}
	appointmentHandler, err := appointment.NewUIHandler(logger, appointmentService, appointmentQuery, prescriptionQuery, profRepo, asstRepo, slotQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to create appointment UI handler: %w", err)
	}

	return appointmentHandler, nil
}

func initializeUIPrescriptionHandler(logger *slog.Logger, pool *pgxpool.Pool, storageClient *storage.Client) (*prescription.UIHandler, error) {
	repo, err := prescription.NewRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create prescription repository: %w", err)
	}
	query, err := prescription.NewQuery(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create prescription query: %w", err)
	}
	svc, err := prescription.NewService(repo, storageClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create prescription service: %w", err)
	}
	prescriptionHandler, err := prescription.NewUIHandler(logger, svc, query)
	if err != nil {
		return nil, fmt.Errorf("failed to create prescription UI handler: %w", err)
	}

	return prescriptionHandler, nil
}
