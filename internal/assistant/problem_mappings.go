package assistant

import (
	"appointment-manager/internal/web"
	"errors"
	"net/http"
)

const (
	problemTypeInvalidAssistantID = "/problems/invalid-assistant-id"

	detailAssistantIDNotFound         = "assistant ID not found"
	detailInvalidAssistantID          = "invalid assistant ID"
	detailAssistantNotFound           = "assistant not found"
	detailAssistantEmailAlreadyExists = "assistant email already exists"
	detailFailedToListAssistants      = "failed to list assistants"
	detailFailedToEncodeListResponse  = "failed to encode assistants response"
	detailFailedToGetAssistant        = "failed to get assistant"
	detailFailedToEncodeGetResponse   = "failed to encode assistant response"
	detailFailedToCreateAssistant     = "failed to create assistant"
)

func problemAssistantIDNotFound(path string) web.ProblemDetail {
	return web.NewProblem(http.StatusBadRequest, problemTypeInvalidAssistantID, detailAssistantIDNotFound, path)
}

func problemInvalidAssistantID(path string) web.ProblemDetail {
	return web.NewProblem(http.StatusBadRequest, problemTypeInvalidAssistantID, detailInvalidAssistantID, path)
}

func problemAssistantNotFound(path string) web.ProblemDetail {
	return web.NewProblem(http.StatusNotFound, web.ProblemTypeResourceNotFound, detailAssistantNotFound, path)
}

func problemListAssistants(path string) web.ProblemDetail {
	return web.NewInternalServerProblem(detailFailedToListAssistants, path)
}

func problemEncodeAssistantsResponse(path string) web.ProblemDetail {
	return web.NewInternalServerProblem(detailFailedToEncodeListResponse, path)
}

func problemGetAssistant(path string) web.ProblemDetail {
	return web.NewInternalServerProblem(detailFailedToGetAssistant, path)
}

func problemEncodeAssistantResponse(path string) web.ProblemDetail {
	return web.NewInternalServerProblem(detailFailedToEncodeGetResponse, path)
}

func problemFromCreateError(err error, path string) web.ProblemDetail {
	switch {
	case isValidationError(err):
		return web.NewProblem(http.StatusUnprocessableEntity, web.ProblemTypeValidationFailed, err.Error(), path)
	case errors.Is(err, ErrEmailAlreadyExists):
		return web.NewProblem(http.StatusConflict, web.ProblemTypeConflict, detailAssistantEmailAlreadyExists, path)
	default:
		return web.NewInternalServerProblem(detailFailedToCreateAssistant, path)
	}
}
