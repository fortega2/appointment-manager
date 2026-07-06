package prescription

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"mime/multipart"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"appointment-manager/internal/domain"
	"appointment-manager/internal/ui/components"

	"github.com/google/uuid"
)

const (
	maxPrescriptionUploadBytes int64         = 10 << 20
	presignExpiry              time.Duration = 15 * time.Minute

	renderSnackbarErrMsg = "error rendering snackbar"
)

type service interface {
	Create(ctx context.Context, patientID uuid.UUID, totalSessions int, file multipart.File, header *multipart.FileHeader) (*Prescription, error)
	Cancel(ctx context.Context, id uuid.UUID) error
	PresignedGetURL(ctx context.Context, id uuid.UUID, expiry time.Duration) (string, error)
}

type UIHandler struct {
	service service
	query   *Query
	logger  *slog.Logger
}

func NewUIHandler(logger *slog.Logger, svc service, query *Query) (*UIHandler, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}
	if isNilService(svc) {
		return nil, ErrNilService
	}
	if query == nil {
		return nil, ErrNilQuery
	}

	return &UIHandler{
		service: svc,
		query:   query,
		logger:  logger,
	}, nil
}

func (h *UIHandler) RegisterUIHandlers(mux *http.ServeMux) {
	mux.Handle("GET /prescriptions", h.showDashboardUIHandler())
	mux.Handle("GET /prescriptions/new", h.showCreateFormUIHandler())
	mux.Handle("POST /prescriptions", h.createUIHandler())
	mux.Handle("POST /prescriptions/{id}/cancel", h.cancelUIHandler())
	mux.Handle("GET /prescriptions/{id}/file", h.fileRedirectUIHandler())
}

func (h *UIHandler) showDashboardUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		balances, err := h.query.ListActiveBalances(ctx)
		if err != nil {
			h.logger.ErrorContext(ctx, "failed to list prescription balances for dashboard", slog.Any("error", err))
			if dashErr := Dashboard([]Balance{}).Render(ctx, w); dashErr != nil {
				h.logger.ErrorContext(ctx, "error rendering prescription dashboard", slog.Any("error", dashErr))
			}
			return
		}

		if err := Dashboard(balances).Render(ctx, w); err != nil {
			h.logger.ErrorContext(ctx, "error rendering prescription dashboard", slog.Any("error", err))
		}
	}
}

func (h *UIHandler) showCreateFormUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		patients, err := h.query.AvailablePatients(ctx)
		if err != nil {
			h.logger.ErrorContext(ctx, "failed to list available patients for prescription form", slog.Any("error", err))
			patients = []PatientOption{}
		}

		if err := Form(patients, "/prescriptions").Render(ctx, w); err != nil {
			h.logger.ErrorContext(ctx, "error rendering prescription create form", slog.Any("error", err))
		}
	}
}

func (h *UIHandler) createUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		req, err := h.parseCreateForm(r, w)
		if err != nil {
			h.logger.WarnContext(ctx, "invalid prescription create form", slog.Any("error", err))
			h.renderTableWithSnackbarError(ctx, w, http.StatusBadRequest, "Invalid form data")
			return
		}
		defer req.file.Close()

		if _, err := h.service.Create(ctx, req.patientID, req.totalSessions, req.file, req.header); err != nil {
			status, msg := resolveCreateProblem(err)
			if status == http.StatusInternalServerError {
				h.logger.ErrorContext(ctx, "failed to create prescription", slog.Any("error", err))
			}
			h.renderTableWithSnackbarError(ctx, w, status, msg)
			return
		}

		w.Header().Set("HX-Trigger", "close-modal")
		if err := components.Snackbar("Prescription created successfully", components.SnackbarSuccess).Render(ctx, w); err != nil {
			h.logger.ErrorContext(ctx, renderSnackbarErrMsg, slog.Any("error", err))
		}
		if err := h.renderUpdatedTable(ctx, w); err != nil {
			h.logger.ErrorContext(ctx, "error rendering prescriptions table after create", slog.Any("error", err))
		}
	}
}

func (h *UIHandler) cancelUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		id, err := parsePrescriptionID(r)
		if err != nil {
			h.renderTableWithSnackbarError(ctx, w, http.StatusBadRequest, "Invalid prescription ID")
			return
		}

		if err := h.service.Cancel(ctx, id); err != nil {
			status, msg := resolveCancelProblem(err)
			if status == http.StatusInternalServerError {
				h.logger.ErrorContext(ctx, "failed to cancel prescription", slog.Any("error", err), slog.String("id", id.String()))
			}
			h.renderTableWithSnackbarError(ctx, w, status, msg)
			return
		}

		if err := components.Snackbar("Prescription cancelled successfully", components.SnackbarSuccess).Render(ctx, w); err != nil {
			h.logger.ErrorContext(ctx, renderSnackbarErrMsg, slog.Any("error", err))
		}
		if err := h.renderUpdatedTable(ctx, w); err != nil {
			h.logger.ErrorContext(ctx, "error rendering prescriptions table after cancel", slog.Any("error", err))
		}
	}
}

func (h *UIHandler) fileRedirectUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		id, err := parsePrescriptionID(r)
		if err != nil {
			http.Error(w, "invalid prescription id", http.StatusBadRequest)
			return
		}

		url, err := h.service.PresignedGetURL(ctx, id, presignExpiry)
		if err != nil {
			if errors.Is(err, ErrPrescriptionNotFound) {
				http.NotFound(w, r)
				return
			}
			h.logger.ErrorContext(ctx, "failed to presign prescription document url", slog.Any("error", err), slog.String("id", id.String()))
			http.Error(w, "failed to load document", http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, url, http.StatusFound) //nolint:gosec // G710 false positive: url is a presigned S3/Garage URL generated server-side by storage.Client, not user input.
	}
}

type createFormRequest struct {
	patientID     uuid.UUID
	totalSessions int
	file          multipart.File
	header        *multipart.FileHeader
}

func (h *UIHandler) parseCreateForm(r *http.Request, w http.ResponseWriter) (*createFormRequest, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPrescriptionUploadBytes)
	if err := r.ParseMultipartForm(maxPrescriptionUploadBytes); err != nil { //nolint:gosec // G120 false positive: request body is already bounded above via http.MaxBytesReader.
		return nil, fmt.Errorf("parse multipart form: %w", err)
	}

	patientID, err := domain.ParseID(r.FormValue("patient_id"))
	if err != nil {
		return nil, fmt.Errorf("invalid patient_id: %w", err)
	}

	totalSessions, err := strconv.Atoi(r.FormValue("total_sessions"))
	if err != nil {
		return nil, fmt.Errorf("invalid total_sessions: %w", err)
	}

	file, header, err := r.FormFile("document")
	if err != nil {
		return nil, fmt.Errorf("missing document file: %w", err)
	}

	return &createFormRequest{
		patientID:     patientID,
		totalSessions: totalSessions,
		file:          file,
		header:        header,
	}, nil
}

func (h *UIHandler) renderUpdatedTable(ctx context.Context, w http.ResponseWriter) error {
	balances, err := h.query.ListActiveBalances(ctx)
	if err != nil {
		h.logger.ErrorContext(ctx, "failed to list prescription balances after operation", slog.Any("error", err))
		return err
	}

	return Table(balances).Render(ctx, w)
}

func (h *UIHandler) renderTableWithSnackbarError(ctx context.Context, w http.ResponseWriter, status int, msg string) {
	if err := components.ShowSnackbar(ctx, components.SnackbarError, w, status, msg); err != nil {
		h.logger.ErrorContext(ctx, renderSnackbarErrMsg, slog.Any("error", err))
	}
	if err := h.renderUpdatedTable(ctx, w); err != nil {
		h.logger.ErrorContext(ctx, "error rendering prescriptions table after error", slog.Any("error", err))
	}
}

func isNilService(s service) bool {
	if s == nil {
		return true
	}

	v := reflect.ValueOf(s)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func parsePrescriptionID(r *http.Request) (uuid.UUID, error) {
	return domain.ParseID(r.PathValue("id"))
}

func resolveCreateProblem(err error) (int, string) {
	switch {
	case errors.Is(err, ErrNilPatientID), errors.Is(err, ErrEmptyFilePath), errors.Is(err, ErrInvalidTotalSessions):
		return http.StatusBadRequest, "Invalid form data"
	case errors.Is(err, ErrUnsupportedFileType):
		return http.StatusUnprocessableEntity, "Unsupported file type"
	case errors.Is(err, ErrInvalidPatient):
		return http.StatusUnprocessableEntity, "Invalid patient selected"
	case errors.Is(err, ErrActivePrescriptionExists):
		return http.StatusConflict, "Patient already has an active prescription"
	default:
		return http.StatusInternalServerError, "Failed to create prescription"
	}
}

func resolveCancelProblem(err error) (int, string) {
	switch {
	case errors.Is(err, ErrPrescriptionNotFound):
		return http.StatusNotFound, "Prescription not found"
	default:
		return http.StatusInternalServerError, "Failed to cancel prescription"
	}
}
