package components

import (
	"context"
	"net/http"
)

func ShowSnackbar(ctx context.Context, st SnackbarType, w http.ResponseWriter, stCode int, msg string) error {
	w.WriteHeader(stCode)
	return Snackbar(msg, st).Render(ctx, w)
}
