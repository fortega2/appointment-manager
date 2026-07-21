package appointment

import (
	"appointment-manager/internal/domain"
	"appointment-manager/internal/tracing"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
)

const (
	defaultPage       = 1
	defaultLimit      = 20
	maxLimit          = 100
	queryParamsFormat = "%w: %q"

	cancelToAbsentWindow = 24 * time.Hour

	tracerName = "appointment-manager/internal/appointment"
)

// tracer is resolved once; the global delegate forwards to the real provider
// installed at start-up, so the three mutating methods share one lookup.
var tracer = otel.Tracer(tracerName)

// businessRuleErrors are the package's expected validation and business-rule
// rejections: normal outcomes of Create, Cancel and Attend given bad input or
// a disallowed state transition, not infrastructure failures. spanError keeps
// spans for these Unset rather than marking them Error, so trace-based
// error-rate alerts do not fire on ordinary rejections.
var businessRuleErrors = []error{
	ErrSlotIDRequired,
	ErrInvalidSlotID,
	ErrPatientIDRequired,
	ErrInvalidPatientID,
	ErrProfessionalIDRequired,
	ErrInvalidProfessionalID,
	ErrAssistantIDRequired,
	ErrInvalidAssistantID,
	ErrInvalidAppointmentReference,
	ErrNoActivePrescription,
	ErrNoRemainingSessions,
	ErrSlotBlocked,
	ErrSlotWithoutAvailability,
	ErrMultipleActiveAppointmentsDetected,
	ErrAppointmentStatusChanged,
	ErrAppointmentCannotAttendNow,
	ErrAppointmentCannotAttendWithStatus,
	ErrAppointmentCannotCancelWithStatus,
}

// spanError returns err unchanged unless it matches one of businessRuleErrors,
// in which case it returns nil so tracing.EndSpan leaves the span's default
// Unset status instead of recording an expected rejection as a failure.
func spanError(err error) error {
	for _, sentinel := range businessRuleErrors {
		if errors.Is(err, sentinel) {
			return nil
		}
	}

	return err
}

type repository interface {
	List(ctx context.Context, filter ListFilter) ([]Appointment, error)
	Create(ctx context.Context, appoint Appointment) (uuid.UUID, error)
	GetWindow(ctx context.Context, appointmentID uuid.UUID) (Window, error)
	UpdateStatus(ctx context.Context, appointmentID uuid.UUID, newStatus, expectedStatus Status) error
}

// Metrics records appointment business events. It is satisfied by
// *metrics.Metrics; a nil value passed to the constructor is replaced by a
// no-op implementation so metrics stay an optional dependency.
type Metrics interface {
	RecordAppointmentCreated()
	RecordAppointmentAttended()
	RecordAppointmentCancelled()
	RecordAppointmentAbsent()
}

type Service struct {
	repo    repository
	now     func() time.Time
	metrics Metrics
}

type ListInput struct {
	Page   string
	Limit  string
	Status string
}

type ListFilter struct {
	Page   int
	Limit  int
	Status Status
}

type CreateInput struct {
	SlotID         string
	PatientID      string
	ProfessionalID string
	AssistantID    string
	Notes          *string
}

type Window struct {
	StartTime time.Time
	EndTime   time.Time
	Status    Status
}

func NewService(repo repository, appointmentMetrics Metrics) (*Service, error) {
	return newServiceWithClock(repo, time.Now, appointmentMetrics)
}

func newServiceWithClock(repo repository, now func() time.Time, appointmentMetrics Metrics) (*Service, error) {
	if repo == nil {
		return nil, ErrNilRepository
	}
	if now == nil {
		now = time.Now
	}
	if appointmentMetrics == nil {
		appointmentMetrics = noopMetrics{}
	}

	return &Service{
		repo:    repo,
		now:     now,
		metrics: appointmentMetrics,
	}, nil
}

func (s *Service) List(ctx context.Context, input ListInput) ([]Appointment, error) {
	filter, err := parseListInput(input)
	if err != nil {
		return nil, err
	}

	return s.repo.List(ctx, filter)
}

func (s *Service) Create(ctx context.Context, input CreateInput) (id uuid.UUID, err error) {
	ctx, span := tracer.Start(ctx, "appointment.Service.Create")
	defer func() { tracing.EndSpan(span, spanError(err)) }()

	slotID, err := parseRequiredID(input.SlotID, ErrSlotIDRequired, ErrInvalidSlotID)
	if err != nil {
		return uuid.Nil, err
	}

	patientID, err := parseRequiredID(input.PatientID, ErrPatientIDRequired, ErrInvalidPatientID)
	if err != nil {
		return uuid.Nil, err
	}

	professionalID, err := parseRequiredID(input.ProfessionalID, ErrProfessionalIDRequired, ErrInvalidProfessionalID)
	if err != nil {
		return uuid.Nil, err
	}

	assistantID, err := parseRequiredID(input.AssistantID, ErrAssistantIDRequired, ErrInvalidAssistantID)
	if err != nil {
		return uuid.Nil, err
	}

	appointmentID, err := s.repo.Create(ctx, *NewAppointment(slotID, patientID, professionalID, assistantID, input.Notes))
	if err != nil {
		return uuid.Nil, err
	}

	s.metrics.RecordAppointmentCreated()

	return appointmentID, nil
}

func (s *Service) Cancel(ctx context.Context, appointmentID uuid.UUID) (err error) {
	ctx, span := tracer.Start(ctx, "appointment.Service.Cancel")
	defer func() { tracing.EndSpan(span, spanError(err)) }()

	window, err := s.repo.GetWindow(ctx, appointmentID)
	if err != nil {
		return err
	}

	if window.Status == StatusCancelled {
		return nil
	}

	if window.Status != StatusConfirmed {
		return ErrAppointmentCannotCancelWithStatus
	}

	now := s.now()
	finalStatus := StatusCancelled
	if !now.Before(window.StartTime.Add(-cancelToAbsentWindow)) {
		finalStatus = StatusAbsent
	}

	if err := s.repo.UpdateStatus(ctx, appointmentID, finalStatus, StatusConfirmed); err != nil {
		return err
	}

	if finalStatus == StatusAbsent {
		s.metrics.RecordAppointmentAbsent()
	} else {
		s.metrics.RecordAppointmentCancelled()
	}

	return nil
}

func (s *Service) Attend(ctx context.Context, appointmentID uuid.UUID) (err error) {
	ctx, span := tracer.Start(ctx, "appointment.Service.Attend")
	defer func() { tracing.EndSpan(span, spanError(err)) }()

	window, err := s.repo.GetWindow(ctx, appointmentID)
	if err != nil {
		return err
	}

	if window.Status == StatusAttended {
		return nil
	}

	if window.Status != StatusConfirmed {
		return ErrAppointmentCannotAttendWithStatus
	}

	now := s.now()
	if now.Before(window.StartTime) || now.After(window.EndTime) {
		return ErrAppointmentCannotAttendNow
	}

	if err := s.repo.UpdateStatus(ctx, appointmentID, StatusAttended, StatusConfirmed); err != nil {
		return err
	}

	s.metrics.RecordAppointmentAttended()

	return nil
}

func parseListInput(input ListInput) (ListFilter, error) {
	pageRaw := strings.TrimSpace(input.Page)
	if pageRaw == "" {
		pageRaw = strconv.Itoa(defaultPage)
	}

	limitRaw := strings.TrimSpace(input.Limit)
	if limitRaw == "" {
		limitRaw = strconv.Itoa(defaultLimit)
	}

	statusRaw := strings.TrimSpace(input.Status)
	if statusRaw == "" {
		statusRaw = fmt.Sprint(StatusConfirmed)
	}

	page, err := strconv.Atoi(pageRaw)
	if err != nil || page < defaultPage {
		return ListFilter{}, fmt.Errorf(queryParamsFormat, ErrInvalidPage, pageRaw)
	}

	limit, err := strconv.Atoi(limitRaw)
	if err != nil || limit < 1 || limit > maxLimit {
		return ListFilter{}, fmt.Errorf(queryParamsFormat, ErrInvalidLimit, limitRaw)
	}

	statusValue, err := strconv.Atoi(statusRaw)
	if err != nil {
		return ListFilter{}, fmt.Errorf(queryParamsFormat, ErrInvalidStatus, statusRaw)
	}

	status, err := parseStatus(statusValue)
	if err != nil {
		return ListFilter{}, err
	}

	return ListFilter{
		Page:   page,
		Limit:  limit,
		Status: status,
	}, nil
}

func parseRequiredID(raw string, requiredErr, invalidErr error) (uuid.UUID, error) {
	if strings.TrimSpace(raw) == "" {
		return uuid.Nil, requiredErr
	}

	parsedID, err := domain.ParseID(raw)
	if err != nil {
		return uuid.Nil, invalidErr
	}

	return parsedID, nil
}

// noopMetrics is the default Metrics used when the service is built without a
// recorder, so business instrumentation is optional and tests need not wire it.
type noopMetrics struct{}

func (noopMetrics) RecordAppointmentCreated() {
	// RecordAppointmentCreated is intentionally empty: no metrics recorder was configured.
}

func (noopMetrics) RecordAppointmentAttended() {
	// RecordAppointmentAttended is intentionally empty: no metrics recorder was configured.
}

func (noopMetrics) RecordAppointmentCancelled() {
	// RecordAppointmentCancelled is intentionally empty: no metrics recorder was configured.
}

func (noopMetrics) RecordAppointmentAbsent() {
	// RecordAppointmentAbsent is intentionally empty: no metrics recorder was configured.
}
