package auth

// Catalog keys for the password reset copy. The Go errors keep their English
// text for the logs; these are what reaches the screen and the mail.
const (
	resetNoticeSentKey string = "auth.forgot.sent"

	resetErrorEmailKey string = "auth.error.reset_email"
	//nolint:gosec // G101 false positive: a message catalog key, not a credential.
	resetErrorTokenKey string = "auth.error.reset_token"
	//nolint:gosec // G101 false positive: a message catalog key, not a credential.
	resetErrorMismatchKey string = "auth.error.reset_mismatch"
	//nolint:gosec // G101 false positive: a message catalog key, not a credential.
	resetErrorTooShortKey string = "auth.error.reset_too_short"
	//nolint:gosec // G101 false positive: a message catalog key, not a credential.
	resetErrorTooLongKey string = "auth.error.reset_too_long"
	//nolint:gosec // G101 false positive: a message catalog key, not a credential.
	resetErrorFailedKey string = "auth.error.reset_failed"

	resetMailSubjectKey  string = "auth.mail.reset.subject"
	resetMailGreetingKey string = "auth.mail.reset.greeting"
	resetMailActionKey   string = "auth.mail.reset.action"
	resetMailFallbackKey string = "auth.mail.reset.fallback"
	resetMailExpiryKey   string = "auth.mail.reset.expiry"
	resetMailIgnoreKey   string = "auth.mail.reset.ignore"
)

const (
	resetRefusedMsg         string = "password reset refused for exceeding its allowance"
	failedDispatchResetMsg  string = "failed to dispatch password reset"
	failedConsumeResetMsg   string = "failed to consume password reset token"
	failedClearSessionsMsg  string = "failed to clear sessions after a password reset"
	failedUpdatePasswordMsg string = "failed to update the password hash"
	renderResetPageMsg      string = "error rendering a password reset page"
)
