package appointment_test

import (
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"appointment-manager/internal/appointment"
	"appointment-manager/internal/i18n"
)

const (
	viewPatientName = "Grace Hopper"
	viewProfName    = "Alan Turing"

	viewStatusES = "Confirmado"
	viewStatusEN = "Confirmed"

	viewTitleES  = "<title>Turnos"
	viewTitleEN  = "<title>Appointments"
	viewEmptyES  = "No hay turnos"
	viewEmptyEN  = "There are no appointments"
	viewCreateES = "Crear turno"
	viewCreateEN = "Create Appointment"
	// The row actions only exist for a confirmed appointment.
	viewAttendES = "Marcar como atendido"
	viewAttendEN = "Mark as Attended"
	viewNotesES  = "Notas"
	viewNotesEN  = "Notes"
)

func renderIn(t *testing.T, locale i18n.Locale, component templ.Component) string {
	t.Helper()

	var body strings.Builder
	require.NoError(t, component.Render(i18n.WithLocale(t.Context(), locale), &body))

	return body.String()
}

func confirmedAppointments() []appointment.List {
	start := time.Date(2026, time.May, 25, 10, 0, 0, 0, time.UTC)

	return []appointment.List{{
		ID:                   "77777777-8888-9999-aaaa-bbbbbbbbbbbb",
		StartTime:            start,
		EndTime:              start.Add(time.Hour),
		PatientFullName:      viewPatientName,
		ProfessionalFullName: viewProfName,
		Status:               appointment.StatusConfirmed,
	}}
}

func TestAppointmentDashboardCopyFollowsTheLocale(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		locale i18n.Locale
		title  string
		create string
		attend string
		status string
	}{
		{"spanish", i18n.LocaleES, viewTitleES, viewCreateES, viewAttendES, viewStatusES},
		{"english", i18n.LocaleEN, viewTitleEN, viewCreateEN, viewAttendEN, viewStatusEN},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body := renderIn(t, tt.locale, appointment.Dashboard(confirmedAppointments()))

			assert.Contains(t, body, tt.title)
			assert.Contains(t, body, tt.create)
			assert.Contains(t, body, tt.attend)
			assert.Contains(t, body, tt.status)
			assert.Contains(t, body, viewPatientName, "data must survive translation")
			assert.Contains(t, body, viewProfName)
		})
	}
}

func TestEveryStatusLabelRendersInEveryLocale(t *testing.T) {
	t.Parallel()

	statuses := []appointment.Status{
		appointment.StatusConfirmed,
		appointment.StatusCancelled,
		appointment.StatusAbsent,
		appointment.StatusAttended,
		appointment.StatusCancelledByClinic,
		appointment.Status(0),
	}

	for _, locale := range []i18n.Locale{i18n.LocaleES, i18n.LocaleEN} {
		t.Run(string(locale), func(t *testing.T) {
			t.Parallel()

			ctx := i18n.WithLocale(t.Context(), locale)
			for _, status := range statuses {
				label := i18n.T(ctx, status.LabelKey())

				assert.NotContains(t, label, "MISSING", "status %d has no copy", status)
				assert.NotEmpty(t, label)
			}
		})
	}
}

func TestAppointmentEmptyStateFollowsTheLocale(t *testing.T) {
	t.Parallel()

	assert.Contains(t, renderIn(t, i18n.LocaleES, appointment.Dashboard(nil)), viewEmptyES)
	assert.Contains(t, renderIn(t, i18n.LocaleEN, appointment.Dashboard(nil)), viewEmptyEN)
}

func TestAppointmentFormCopyFollowsTheLocale(t *testing.T) {
	t.Parallel()

	slots := []appointment.SlotOptionDTO{{ID: "slot-1", FallbackLabel: "2026-05-25 10:00"}}
	patients := []appointment.PatientOptionDTO{{ID: "pat-1", Label: viewPatientName, RemainingSessions: 3}}

	form := func(locale i18n.Locale) string {
		return renderIn(t, locale, appointment.Form(appointment.FormRequest{}, "/appointments", slots, patients))
	}

	assert.Contains(t, form(i18n.LocaleES), viewNotesES)
	assert.Contains(t, form(i18n.LocaleEN), viewNotesEN)
	assert.Contains(t, form(i18n.LocaleES), viewPatientName, "option labels must survive translation")
}
