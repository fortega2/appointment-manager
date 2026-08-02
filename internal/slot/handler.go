package slot

import (
	"appointment-manager/internal/professional"
	"appointment-manager/internal/ui/components"
	"appointment-manager/internal/web"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// cancelAppointmentsFunc cancels every confirmed appointment booked on a slot.
// It is a function rather than a package dependency because internal/appointment
// already imports internal/slot, so importing it back would be a cycle; the real
// implementation is bound in cmd/server.
type cancelAppointmentsFunc func(ctx context.Context, slotID uuid.UUID) error

// sendNotificationFunc announces that a slot was cancelled. It is declared here,
// in the consumer, so the notification transport (email, SMS, push) can change
// without this package knowing. It is called on the request goroutine, so
// implementations must not block: they are expected to hand the work off and
// return, keeping delivery and its lifetime entirely on their own side.
type sendNotificationFunc func(ctx context.Context, slotID uuid.UUID)

type Handler struct {
	logger             *slog.Logger
	repo               *Repository
	query              *Query
	pRepo              *professional.Repository
	cancelAppointments cancelAppointmentsFunc
	sendNotification   sendNotificationFunc
}

func NewHandler(
	logger *slog.Logger,
	repo *Repository,
	query *Query,
	pRepo *professional.Repository,
	cancelAppointments cancelAppointmentsFunc,
	sendNotification sendNotificationFunc,
) (*Handler, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}

	if repo == nil {
		return nil, ErrNilRepository
	}

	if query == nil {
		return nil, ErrNilQuery
	}

	if pRepo == nil {
		return nil, ErrNilProfessionalRepo
	}

	if cancelAppointments == nil {
		return nil, ErrNilCancelAppointments
	}

	if sendNotification == nil {
		return nil, ErrNilSendNotification
	}

	return &Handler{
		logger:             logger,
		repo:               repo,
		query:              query,
		pRepo:              pRepo,
		cancelAppointments: cancelAppointments,
		sendNotification:   sendNotification,
	}, nil
}

func (h *Handler) RegisterUIHandlers(mux web.Mux) {
	mux.Handle("GET /slots", h.showDashboardUIHandler())
	mux.Handle("GET /slots/new", h.showCreateFormUIHandler())
	mux.Handle("POST /slots", h.createUIHandler())
	mux.Handle("DELETE /slots/{id}", h.cancelUIHandler())
}

func (h *Handler) showDashboardUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		dto, err := h.query.List(ctx)
		if err != nil {
			h.logger.ErrorContext(ctx, "failed to list slots", slog.Any("error", err))
			h.createSnackbarError(ctx, w, http.StatusInternalServerError, "Failed to load slots", "slot.Query.List")
		}

		professionals, err := h.pRepo.List(ctx)
		if err != nil {
			h.logger.ErrorContext(ctx, "failed to list professionals", slog.Any("error", err))
			h.createSnackbarError(ctx, w, http.StatusInternalServerError, "Failed to load professionals", "professional.Query.List")
		}

		pDTO := make([]ProfessionalDTO, len(professionals))
		for i, p := range professionals {
			pDTO[i] = ProfessionalDTO{
				ID:        p.ID.String(),
				FirstName: p.FirstName,
				LastName:  p.LastName,
			}
		}

		if err := Dashboard(dto, pDTO).Render(ctx, w); err != nil {
			h.logger.ErrorContext(ctx, "failed to render dashboard", slog.Any("error", err))
			h.createSnackbarError(ctx, w, http.StatusInternalServerError, "Failed to load dashboard", "Dashboard.Render")
		}
	}
}

func (h *Handler) createSnackbarError(ctx context.Context, w http.ResponseWriter, statusCode int, message, operation string) {
	if err := components.ShowSnackbarOnly(ctx, components.SnackbarError, w, statusCode, message); err != nil {
		h.logger.ErrorContext(ctx, "error rendering snackbar", slog.Any("error", err), slog.String("package", "slot"), slog.String("operation", operation))
	}
}

func (h *Handler) showCreateFormUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		professionals, err := h.pRepo.List(ctx)
		if err != nil {
			h.logger.ErrorContext(ctx, "failed to list professionals for form", slog.Any("error", err))
			h.createSnackbarError(ctx, w, http.StatusInternalServerError, "Failed to load professionals", "pRepo.List")
			professionals = []professional.Professional{}
		}

		pDTO := make([]ProfessionalDTO, len(professionals))
		for i, p := range professionals {
			pDTO[i] = ProfessionalDTO{
				ID:        p.ID.String(),
				FirstName: p.FirstName,
				LastName:  p.LastName,
			}
		}

		if err := Form(ListDTO{}, "/slots", pDTO).Render(ctx, w); err != nil {
			h.logger.ErrorContext(ctx, "error rendering slot create form", slog.Any("error", err))
		}
	}
}

type formRequest struct {
	professionalID uuid.UUID
	startTime      time.Time
	endTime        time.Time
	maxCapacity    int16
}

// date returns the slot's calendar date, derived from startTime's UTC day
// rather than a separately submitted field, so it is always consistent with
// startTime regardless of the visitor's timezone.
func (f *formRequest) date() time.Time {
	y, m, d := f.startTime.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, f.startTime.Location())
}

func (h *Handler) parseForm(r *http.Request, w http.ResponseWriter) (*formRequest, error) {
	const requestBodyMaxBytes int64 = 1 << 20
	r.Body = http.MaxBytesReader(w, r.Body, requestBodyMaxBytes)
	if err := r.ParseForm(); err != nil {
		return nil, fmt.Errorf("parse form: %w", err)
	}

	profIDStr := r.FormValue("professional_id")
	profID, err := uuid.Parse(profIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid professional_id: %w", err)
	}

	startTime, err := time.Parse(time.RFC3339, r.FormValue("start_time"))
	if err != nil {
		return nil, fmt.Errorf("invalid start_time format: %w", err)
	}

	endTime, err := time.Parse(time.RFC3339, r.FormValue("end_time"))
	if err != nil {
		return nil, fmt.Errorf("invalid end_time format: %w", err)
	}

	maxCapStr := r.FormValue("max_capacity")
	maxCapInt, err := strconv.ParseInt(maxCapStr, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("invalid max_capacity: %w", err)
	}

	return &formRequest{
		professionalID: profID,
		startTime:      startTime.UTC(),
		endTime:        endTime.UTC(),
		maxCapacity:    int16(maxCapInt),
	}, nil
}

func (h *Handler) createUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		req, err := h.parseForm(r, w)
		if err != nil {
			h.logger.ErrorContext(ctx, "error parsing slot create form", slog.Any("error", err))
			h.createSnackbarError(ctx, w, http.StatusBadRequest, "Failed to parse form data", "parseForm")
			return
		}

		s, err := NewSlot(req.professionalID, req.date(), req.startTime, req.endTime, req.maxCapacity)
		if err != nil {
			h.logger.ErrorContext(ctx, "error creating slot from form data", slog.Any("error", err))
			h.createSnackbarError(ctx, w, http.StatusUnprocessableEntity, err.Error(), "NewSlot")
			return
		}

		if err := h.repo.Create(ctx, s); err != nil {
			h.logger.ErrorContext(ctx, "failed to create slot", slog.Any("error", err))
			if errors.Is(err, ErrSlotOverlaps) {
				h.createSnackbarError(ctx, w, http.StatusConflict, "The professional already has an appointment within this time range.", "repo.Create")
				return
			}
			h.createSnackbarError(ctx, w, http.StatusInternalServerError, "Failed to create slot", "repo.Create")
			return
		}

		h.renderUpdatedSlotsTable(ctx, w, "Slot created successfully")
	}
}

func (h *Handler) cancelUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		idStr := r.PathValue("id")
		id, err := uuid.Parse(idStr)
		if err != nil {
			h.createSnackbarError(ctx, w, http.StatusBadRequest, "Invalid slot ID", "uuid.Parse")
			return
		}

		const cancelOperation = "repo.Cancel"
		if err := h.repo.Cancel(ctx, id); err != nil {
			h.logger.ErrorContext(ctx, "failed to cancel slot", slog.Any("error", err), slog.String("id", idStr))
			switch {
			case errors.Is(err, ErrSlotNotFound):
				h.createSnackbarError(ctx, w, http.StatusNotFound, "Slot not found", cancelOperation)
			case errors.Is(err, ErrSlotAlreadyCancelled):
				h.createSnackbarError(ctx, w, http.StatusConflict, "This slot has already been cancelled.", cancelOperation)
			default:
				h.createSnackbarError(ctx, w, http.StatusInternalServerError, "Failed to cancel slot", cancelOperation)
			}
			return
		}

		// Only infrastructure errors reach this point: the update underneath is a
		// bulk one, so cancelling zero appointments is an ordinary result. The
		// slot is already blocked here, leaving the pair inconsistent, and the
		// slot can no longer be cancelled again so a retry cannot repair it.
		// The reconciliation sweep converges that state on its next tick.
		if err := h.cancelAppointments(ctx, id); err != nil {
			h.logger.ErrorContext(ctx, "failed to cancel appointments for slot", slog.Any("error", err), slog.String("slot_id", idStr))
			h.createSnackbarError(ctx, w, http.StatusInternalServerError, "Failed to cancel associated appointments", "cancelAppointments")

			return
		}

		h.sendNotification(ctx, id)
		h.renderUpdatedSlotsTable(ctx, w, "Slot canceled successfully")
	}
}

func (h *Handler) renderUpdatedSlotsTable(ctx context.Context, w http.ResponseWriter, successMsg string) {
	dto, err := h.query.List(ctx)
	if err != nil {
		h.logger.ErrorContext(ctx, "failed to list slots after operation", slog.Any("error", err))
		h.createSnackbarError(ctx, w, http.StatusInternalServerError, "Failed to load slots", "query.List")
		return
	}

	w.Header().Set("HX-Trigger", "close-modal")
	if err := components.Snackbar(successMsg, components.SnackbarSuccess).Render(ctx, w); err != nil {
		h.logger.ErrorContext(ctx, "error rendering success snackbar after slot operation", slog.Any("error", err))
	}
	if err := Table(dto).Render(ctx, w); err != nil {
		h.logger.ErrorContext(ctx, "error rendering slots table after operation", slog.Any("error", err))
	}
}
