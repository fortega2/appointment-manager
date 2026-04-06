package appointment

import (
	"appointment-manager/internal/web"
	"errors"
	"net/http"
)

const (
	detailInvalidListQueryParams = "invalid list query parameters"
	detailFailedToList           = "failed to fetch appointments"
	detailFailedToEncodeList     = "failed to encode appointments response"
	detailFailedToCreate         = "failed to create appointment"
	detailAppointmentIDRequired  = "appointment ID is required"
	detailFailedToCancel         = "failed to cancel appointment"
	detailFailedToAttend         = "failed to attend appointment"
)

func problemInvalidListQueryParams(path string) web.ProblemDetail {
	return web.NewProblem(
		http.StatusBadRequest,
		web.ProblemTypeValidationFailed,
		detailInvalidListQueryParams,
		path,
	)
}

func problemListAppointments(path string) web.ProblemDetail {
	return web.NewInternalServerProblem(detailFailedToList, path)
}

func problemEncodeAppointmentsResponse(path string) web.ProblemDetail {
	return web.NewInternalServerProblem(detailFailedToEncodeList, path)
}

func problemFromCreateError(err error, path string) web.ProblemDetail {
	switch {
	case isCreateValidationError(err):
		return web.NewProblem(http.StatusUnprocessableEntity, web.ProblemTypeValidationFailed, err.Error(), path)
	case errors.Is(err, ErrMultipleActiveAppointmentsDetected),
		errors.Is(err, ErrSlotBlocked),
		errors.Is(err, ErrSlotWithoutAvailability):
		return web.NewProblem(http.StatusConflict, web.ProblemTypeConflict, err.Error(), path)
	case errors.Is(err, ErrInvalidAppointmentReference):
		return web.NewProblem(http.StatusUnprocessableEntity, web.ProblemTypeValidationFailed, err.Error(), path)
	default:
		return web.NewInternalServerProblem(detailFailedToCreate, path)
	}
}

func problemAppointmentIDRequired(path string) web.ProblemDetail {
	return web.NewProblem(http.StatusBadRequest, web.ProblemTypeValidationFailed, detailAppointmentIDRequired, path)
}

func problemInvalidAppointmentID(rawID, path string) web.ProblemDetail {
	return web.NewProblem(
		http.StatusBadRequest,
		web.ProblemTypeValidationFailed,
		formatInvalidID(rawID),
		path,
	)
}

func problemFromCancelError(err error, path string) web.ProblemDetail {
	switch {
	case errors.Is(err, ErrInvalidAppointmentReference):
		return web.NewProblem(http.StatusNotFound, web.ProblemTypeResourceNotFound, err.Error(), path)
	case errors.Is(err, ErrAppointmentCannotCancelWithStatus):
		return web.NewProblem(http.StatusConflict, web.ProblemTypeConflict, err.Error(), path)
	case errors.Is(err, ErrAppointmentStatusChanged):
		return web.NewProblem(http.StatusConflict, web.ProblemTypeConflict, err.Error(), path)
	default:
		return web.NewInternalServerProblem(detailFailedToCancel, path)
	}
}

func problemFromAttendError(err error, path string) web.ProblemDetail {
	switch {
	case errors.Is(err, ErrInvalidAppointmentReference):
		return web.NewProblem(http.StatusNotFound, web.ProblemTypeResourceNotFound, err.Error(), path)
	case errors.Is(err, ErrAppointmentCannotAttendWithStatus):
		return web.NewProblem(http.StatusConflict, web.ProblemTypeConflict, err.Error(), path)
	case errors.Is(err, ErrAppointmentStatusChanged):
		return web.NewProblem(http.StatusConflict, web.ProblemTypeConflict, err.Error(), path)
	case errors.Is(err, ErrAppointmentCannotAttendNow):
		return web.NewProblem(http.StatusUnprocessableEntity, web.ProblemTypeValidationFailed, err.Error(), path)
	default:
		return web.NewInternalServerProblem(detailFailedToAttend, path)
	}
}
