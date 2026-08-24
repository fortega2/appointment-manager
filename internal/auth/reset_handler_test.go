package auth

import (
	"appointment-manager/internal/assistant"
	"appointment-manager/internal/i18n"
	"appointment-manager/internal/mailer"
	"appointment-manager/internal/password"
	"appointment-manager/internal/passwordreset"
	"appointment-manager/internal/session"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	forgotPath = "/forgot-password"
	resetPath  = "/reset-password"

	knownEmail   = "assistant@email.com"
	unknownEmail = "nobody@email.com"
	strongPass   = "a-long-enough-passphrase"
	otherPass    = "another-long-passphrase"
	resetBaseURL = "https://turnos.example.com"
	resetTTL     = 30 * time.Minute

	formType = "application/x-www-form-urlencoded"
)

type stubMailer struct {
	mu   sync.Mutex
	sent []mailer.Message
}

func (m *stubMailer) Send(_ context.Context, msg mailer.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sent = append(m.sent, msg)

	return nil
}

func (m *stubMailer) messages() []mailer.Message {
	m.mu.Lock()
	defer m.mu.Unlock()

	return append([]mailer.Message(nil), m.sent...)
}

type stubResetRepo struct {
	mu      sync.Mutex
	account *assistant.Assistant
	newHash string
}

func (r *stubResetRepo) GetByEmail(_ context.Context, email string) (*assistant.Assistant, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.account == nil || r.account.Email != email {
		return nil, assistant.ErrAssistantNotFound
	}
	copied := *r.account

	return &copied, nil
}

func (r *stubResetRepo) UpdatePasswordHash(_ context.Context, _ uuid.UUID, passwordHash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.newHash = passwordHash

	return nil
}

func (r *stubResetRepo) storedHash() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.newHash
}

type stubTokenStorer struct {
	tokens map[string]passwordreset.Token
}

func newStubTokenStorer() *stubTokenStorer {
	return &stubTokenStorer{tokens: make(map[string]passwordreset.Token)}
}

func (s *stubTokenStorer) Create(_ context.Context, token passwordreset.Token) error {
	for id, existing := range s.tokens {
		if existing.AssistantID == token.AssistantID {
			delete(s.tokens, id)
		}
	}
	s.tokens[token.ID] = token

	return nil
}

func (s *stubTokenStorer) Get(_ context.Context, id string) (*passwordreset.Token, error) {
	token, ok := s.tokens[id]
	if !ok {
		return nil, passwordreset.ErrTokenNotFound
	}
	copied := token

	return &copied, nil
}

func (s *stubTokenStorer) Consume(_ context.Context, id string) (*passwordreset.Token, error) {
	token, ok := s.tokens[id]
	if !ok {
		return nil, passwordreset.ErrTokenNotFound
	}
	delete(s.tokens, id)
	copied := token

	return &copied, nil
}

func (s *stubTokenStorer) DeleteExpired(_ context.Context, before time.Time) (int64, error) {
	removed := int64(0)
	for id, token := range s.tokens {
		if before.After(token.ExpiresAt) {
			delete(s.tokens, id)
			removed++
		}
	}

	return removed, nil
}

type resetFixture struct {
	mux      *http.ServeMux
	mail     *stubMailer
	repo     *stubResetRepo
	sessions *session.Store
	waiters  *sync.WaitGroup
	account  assistant.Assistant
}

func newResetFixture(t *testing.T) *resetFixture {
	t.Helper()

	return newResetFixtureWithBurst(t, roomyBurst)
}

func newResetFixtureWithBurst(t *testing.T, burst int) *resetFixture {
	t.Helper()

	account := assistant.Assistant{
		ID:           uuid.Must(uuid.NewV7()),
		FirstName:    "Ana",
		LastName:     "Gomez",
		Email:        knownEmail,
		PasswordHash: "old-hash",
	}

	storer := newStubTokenStorer()
	tokens, err := passwordreset.NewStore(storer, resetTTL)
	require.NoError(t, err)

	sessions := newTestSessionStore(t)
	stubMail := &stubMailer{}
	repo := &stubResetRepo{account: &account}
	waiters := &sync.WaitGroup{}

	handler, err := NewResetHandler(ResetHandlerConfig{
		Logger:   slog.New(slog.DiscardHandler),
		Tokens:   tokens,
		Sessions: sessions,
		Repo:     repo,
		Hasher:   password.NewArgon2(nil),
		Mail:     stubMail,
		Limiter:  newLimiterWithBurst(t, burst),
		Waiters:  waiters,
		BaseURL:  resetBaseURL,
	})
	require.NoError(t, err)

	mux := http.NewServeMux()
	handler.RegisterHandlers(mux)

	return &resetFixture{
		mux:      mux,
		mail:     stubMail,
		repo:     repo,
		sessions: sessions,
		waiters:  waiters,
		account:  account,
	}
}

// post drives a registered route and waits for any detached dispatch, so the
// assertions see the goroutine's effects rather than racing them.
func (f *resetFixture) post(t *testing.T, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()

	ctx := i18n.WithLocale(t.Context(), i18n.LocaleES)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", formType)

	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	f.waiters.Wait()

	return rec
}

func (f *resetFixture) get(t *testing.T, target string) *httptest.ResponseRecorder {
	t.Helper()

	ctx := i18n.WithLocale(t.Context(), i18n.LocaleES)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, target, nil)

	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)

	return rec
}

// issueToken puts a live token in the store the way a real request would, and
// returns the raw value the mail would have carried.
func (f *resetFixture) issueToken(t *testing.T) string {
	t.Helper()

	rec := f.post(t, forgotPath, url.Values{"email": {knownEmail}})
	require.Equal(t, http.StatusOK, rec.Code)

	sent := f.mail.messages()
	require.Len(t, sent, 1)

	return tokenFromMail(t, sent[0])
}

func tokenFromMail(t *testing.T, msg mailer.Message) string {
	t.Helper()

	for line := range strings.SplitSeq(msg.TextBody, "\n") {
		if !strings.HasPrefix(line, resetBaseURL) {
			continue
		}
		parsed, err := url.Parse(strings.TrimSpace(line))
		require.NoError(t, err)

		token := parsed.Query().Get("token")
		require.NotEmpty(t, token)

		return token
	}

	t.Fatalf("no reset link in the mail body: %q", msg.TextBody)

	return ""
}

func TestNewResetHandlerValidation(t *testing.T) {
	t.Parallel()

	valid := func() ResetHandlerConfig {
		storer := newStubTokenStorer()
		tokens, err := passwordreset.NewStore(storer, resetTTL)
		require.NoError(t, err)
		return ResetHandlerConfig{
			Logger:   slog.New(slog.DiscardHandler),
			Tokens:   tokens,
			Sessions: newTestSessionStore(t),
			Repo:     &stubResetRepo{},
			Hasher:   password.NewArgon2(nil),
			Mail:     &stubMailer{},
			Limiter:  newTestLimiter(t),
			Waiters:  &sync.WaitGroup{},
			BaseURL:  resetBaseURL,
		}
	}

	tests := []struct {
		mutate   func(*ResetHandlerConfig)
		expected error
		name     string
	}{
		{name: "nil logger", mutate: func(c *ResetHandlerConfig) { c.Logger = nil }, expected: ErrNilLogger},
		{name: "nil token store", mutate: func(c *ResetHandlerConfig) { c.Tokens = nil }, expected: ErrNilResetTokenStore},
		{name: "nil session store", mutate: func(c *ResetHandlerConfig) { c.Sessions = nil }, expected: ErrNilSessionStore},
		{name: "nil repo", mutate: func(c *ResetHandlerConfig) { c.Repo = nil }, expected: ErrNilAssistantRepo},
		{name: "nil hasher", mutate: func(c *ResetHandlerConfig) { c.Hasher = nil }, expected: ErrNilPasswordHasher},
		{name: "nil mailer", mutate: func(c *ResetHandlerConfig) { c.Mail = nil }, expected: ErrNilMailer},
		{name: "nil limiter", mutate: func(c *ResetHandlerConfig) { c.Limiter = nil }, expected: ErrNilRateLimiter},
		{name: "nil wait group", mutate: func(c *ResetHandlerConfig) { c.Waiters = nil }, expected: ErrNilWaitGroup},
		{name: "blank base url", mutate: func(c *ResetHandlerConfig) { c.BaseURL = "   " }, expected: ErrEmptyBaseURL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := valid()
			tt.mutate(&cfg)

			handler, err := NewResetHandler(cfg)
			require.Error(t, err)
			assert.Nil(t, handler)
			assert.ErrorIs(t, err, tt.expected)
		})
	}
}

// TestForgotPasswordAnswersIdenticallyForAnUnknownEmail is the anti-enumeration
// guarantee: the two responses must not differ by a single byte.
func TestForgotPasswordAnswersIdenticallyForAnUnknownEmail(t *testing.T) {
	t.Parallel()

	fixture := newResetFixture(t)

	known := fixture.post(t, forgotPath, url.Values{"email": {knownEmail}})
	unknown := fixture.post(t, forgotPath, url.Values{"email": {unknownEmail}})

	assert.Equal(t, known.Code, unknown.Code)
	assert.Equal(t, known.Body.Bytes(), unknown.Body.Bytes())
	assert.Equal(t, http.StatusOK, known.Code)
}

func TestForgotPasswordMailsAKnownAddress(t *testing.T) {
	t.Parallel()

	fixture := newResetFixture(t)
	fixture.post(t, forgotPath, url.Values{"email": {knownEmail}})

	sent := fixture.mail.messages()
	require.Len(t, sent, 1)

	assert.Equal(t, knownEmail, sent[0].To)
	assert.NotEmpty(t, sent[0].Subject)
	assert.Contains(t, sent[0].TextBody, resetBaseURL+resetPath)
	assert.Contains(t, sent[0].HTMLBody, resetBaseURL+resetPath)
}

// TestForgotPasswordMailsNobodyForAnUnknownAddress is the other half of the
// guarantee: identical answers must not mean a mail went somewhere.
func TestForgotPasswordMailsNobodyForAnUnknownAddress(t *testing.T) {
	t.Parallel()

	fixture := newResetFixture(t)
	fixture.post(t, forgotPath, url.Values{"email": {unknownEmail}})

	assert.Empty(t, fixture.mail.messages())
}

func TestForgotPasswordRequiresAnEmail(t *testing.T) {
	t.Parallel()

	fixture := newResetFixture(t)
	rec := fixture.post(t, forgotPath, url.Values{"email": {"   "}})

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, fixture.mail.messages())
}

func TestResetPasswordPageRejectsAnUnknownToken(t *testing.T) {
	t.Parallel()

	fixture := newResetFixture(t)
	rec := fixture.get(t, resetPath+"?token=not-a-real-token")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), i18n.T(t.Context(), "auth.reset.expired_title"))
	assert.NotContains(t, rec.Body.String(), `name="password"`)
}

func TestResetPasswordPageSetsNoReferrer(t *testing.T) {
	t.Parallel()

	fixture := newResetFixture(t)
	token := fixture.issueToken(t)

	rec := fixture.get(t, resetPath+"?token="+url.QueryEscape(token))

	assert.Equal(t, "no-referrer", rec.Header().Get("Referrer-Policy"))
	assert.Contains(t, rec.Body.String(), `name="password"`)
}

func TestConfirmResetRejectsMismatchedPasswords(t *testing.T) {
	t.Parallel()

	fixture := newResetFixture(t)
	token := fixture.issueToken(t)

	rec := fixture.post(t, resetPath, url.Values{
		"token":                 {token},
		"password":              {strongPass},
		"password_confirmation": {otherPass},
	})

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, fixture.repo.storedHash())
}

// TestConfirmResetKeepsTheTokenOnAWeakPassword pins the ordering: validation
// runs before Consume, so a rejected password costs a retry and not a new mail.
func TestConfirmResetKeepsTheTokenOnAWeakPassword(t *testing.T) {
	t.Parallel()

	fixture := newResetFixture(t)
	token := fixture.issueToken(t)

	rec := fixture.post(t, resetPath, url.Values{
		"token":                 {token},
		"password":              {"short"},
		"password_confirmation": {"short"},
	})
	require.Equal(t, http.StatusBadRequest, rec.Code)

	retry := fixture.post(t, resetPath, url.Values{
		"token":                 {token},
		"password":              {strongPass},
		"password_confirmation": {strongPass},
	})
	assert.Equal(t, http.StatusOK, retry.Code)
}

func TestConfirmResetSpendsTheTokenOnce(t *testing.T) {
	t.Parallel()

	fixture := newResetFixture(t)
	token := fixture.issueToken(t)

	form := url.Values{
		"token":                 {token},
		"password":              {strongPass},
		"password_confirmation": {strongPass},
	}
	require.Equal(t, http.StatusOK, fixture.post(t, resetPath, form).Code)

	replay := fixture.post(t, resetPath, form)
	assert.Equal(t, http.StatusBadRequest, replay.Code)
}

func TestConfirmResetStoresAVerifiableHashAndClearsSessions(t *testing.T) {
	t.Parallel()

	fixture := newResetFixture(t)

	live, err := fixture.sessions.Create(t.Context(), fixture.account.ID.String())
	require.NoError(t, err)

	token := fixture.issueToken(t)
	rec := fixture.post(t, resetPath, url.Values{
		"token":                 {token},
		"password":              {strongPass},
		"password_confirmation": {strongPass},
	})

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "/login", rec.Header().Get("HX-Redirect"))
	assert.Empty(t, rec.Header().Get("Set-Cookie"), "a reset must not log anybody in")

	stored := fixture.repo.storedHash()
	require.NotEmpty(t, stored)
	require.NotEqual(t, fixture.account.PasswordHash, stored)

	match, err := password.NewArgon2(nil).Compare(t.Context(), stored, strongPass)
	require.NoError(t, err)
	assert.True(t, match, "the login must accept the password the reset stored")

	_, err = fixture.sessions.Get(t.Context(), live)
	assert.ErrorIs(t, err, session.ErrSessionNotFound)
}

func TestForgotPasswordPageRenders(t *testing.T) {
	t.Parallel()

	fixture := newResetFixture(t)
	rec := fixture.get(t, forgotPath)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `hx-post="/forgot-password"`)
}

// TestForgotPasswordThrottlesRepeatedRequests keeps the reset from becoming a
// way to have somebody's inbox flooded.
func TestForgotPasswordThrottlesRepeatedRequests(t *testing.T) {
	t.Parallel()

	fixture := newResetFixtureWithBurst(t, 1)
	form := url.Values{"email": {knownEmail}}

	require.Equal(t, http.StatusOK, fixture.post(t, forgotPath, form).Code)

	throttled := fixture.post(t, forgotPath, form)
	assert.Equal(t, http.StatusTooManyRequests, throttled.Code)
	assert.NotEmpty(t, throttled.Header().Get("Retry-After"))
	assert.Len(t, fixture.mail.messages(), 1)
}

// TestConfirmResetThrottlesRepeatedAttempts keeps the redeem route from being a
// free way to hold the Argon2 slots the login shares.
func TestConfirmResetThrottlesRepeatedAttempts(t *testing.T) {
	t.Parallel()

	fixture := newResetFixtureWithBurst(t, 1)
	form := url.Values{
		"token":                 {fixture.issueToken(t)},
		"password":              {strongPass},
		"password_confirmation": {strongPass},
	}

	require.Equal(t, http.StatusOK, fixture.post(t, resetPath, form).Code)

	throttled := fixture.post(t, resetPath, form)
	assert.Equal(t, http.StatusTooManyRequests, throttled.Code)
	assert.NotEmpty(t, throttled.Header().Get("Retry-After"))
}

func TestConfirmResetRejectsAnOverlongPassword(t *testing.T) {
	t.Parallel()

	fixture := newResetFixture(t)
	token := fixture.issueToken(t)

	long := strings.Repeat("a", password.MaxLength+1)
	rec := fixture.post(t, resetPath, url.Values{
		"token":                 {token},
		"password":              {long},
		"password_confirmation": {long},
	})

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, fixture.repo.storedHash())
}
