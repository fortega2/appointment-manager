package auth

import (
	"appointment-manager/internal/i18n"
	"appointment-manager/internal/mailer"
	"context"
	"fmt"
	"html/template"
	"net/url"
	"strings"
	"time"
)

// resetMailHTML carries its own styling because mail clients drop external
// stylesheets. It lives here, not in internal/ui, so Tailwind never scans it.
var resetMailHTML = template.Must(template.New("reset").Parse(`<html>
<body style="margin:0;padding:24px;background:#f3f4f6;font-family:Arial,Helvetica,sans-serif;color:#111827">
<div style="max-width:480px;margin:0 auto;background:#ffffff;padding:32px;border-radius:8px">
<p style="margin:0 0 24px;font-size:15px;line-height:1.5">{{ .Greeting }}</p>
<p style="margin:0 0 24px">
<a href="{{ .URL }}" style="display:block;padding:12px 24px;background:#4f46e5;color:#ffffff;text-decoration:none;border-radius:6px;font-size:15px;font-weight:bold;text-align:center">{{ .Action }}</a>
</p>
<p style="margin:0 0 8px;font-size:13px;color:#6b7280">{{ .Fallback }}</p>
<p style="margin:0 0 24px;font-size:13px;word-break:break-all"><a href="{{ .URL }}" style="color:#4f46e5">{{ .URL }}</a></p>
<p style="margin:0 0 8px;font-size:13px;color:#6b7280">{{ .Expiry }}</p>
<p style="margin:0;font-size:13px;color:#6b7280">{{ .Ignore }}</p>
</div>
</body>
</html>`))

type resetMailData struct {
	Greeting string
	Action   string
	URL      string
	Fallback string
	Expiry   string
	Ignore   string
}

// resetMessage builds the mail in the requester's locale. Both parts go out: the
// HTML one turns the link into a button, the text one is the fallback.
func (h *ResetHandler) resetMessage(ctx context.Context, recipient, token string) (mailer.Message, error) {
	data := resetMailData{
		Greeting: i18n.T(ctx, resetMailGreetingKey),
		Action:   i18n.T(ctx, resetMailActionKey),
		URL:      h.resetURL(token),
		Fallback: i18n.T(ctx, resetMailFallbackKey),
		Expiry:   i18n.T(ctx, resetMailExpiryKey, i18n.M{"minutes": int(h.tokenTTL / time.Minute)}),
		Ignore:   i18n.T(ctx, resetMailIgnoreKey),
	}

	var html strings.Builder
	if err := resetMailHTML.Execute(&html, data); err != nil {
		return mailer.Message{}, fmt.Errorf("render reset mail: %w", err)
	}

	return mailer.Message{
		To:      recipient,
		Subject: i18n.T(ctx, resetMailSubjectKey),
		TextBody: strings.Join([]string{
			data.Greeting,
			data.URL,
			data.Expiry,
			data.Ignore,
		}, "\n\n"),
		HTMLBody: html.String(),
	}, nil
}

func (h *ResetHandler) resetURL(token string) string {
	return fmt.Sprintf("%s/reset-password?token=%s", h.baseURL, url.QueryEscape(token))
}
