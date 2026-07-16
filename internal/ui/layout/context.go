package layout

import "context"

type prescriptionsKey struct{}

// WithPrescriptions records whether the prescription UI is available for this
// request. The routes are only registered when object storage is configured,
// so the nav hides the link otherwise to avoid a dead link.
func WithPrescriptions(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, prescriptionsKey{}, enabled)
}

// prescriptionsEnabled reports whether the Prescriptions nav link should be
// rendered. It defaults to false when unset so a request that never passed
// through the middleware hides the link rather than linking nowhere.
func prescriptionsEnabled(ctx context.Context) bool {
	enabled, _ := ctx.Value(prescriptionsKey{}).(bool)
	return enabled
}
