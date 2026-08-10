package prescription_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"appointment-manager/internal/i18n"
	"appointment-manager/internal/i18n/i18ntest"
	"appointment-manager/internal/prescription"
)

const (
	viewPatientName = "Rosalind Franklin"
	viewPrescID     = "99999999-1111-2222-3333-444444444444"

	viewTitleES   = "<title>Recetas"
	viewTitleEN   = "<title>Prescriptions"
	viewEmptyES   = "No hay recetas activas"
	viewEmptyEN   = "There are no active prescriptions"
	viewCreateES  = "Cargar receta"
	viewCreateEN  = "Load Prescription"
	viewConfirmES = "¿Cancelar esta receta?"
	viewConfirmEN = "Cancel this prescription?"
	// Delimited: "Documento" contains "Document", so a bare substring check
	// would pass on Spanish output while asserting the English label.
	viewDocES = ">Documento<"
	viewDocEN = ">Document<"
)

func sampleBalances() []prescription.Balance {
	return []prescription.Balance{{
		PatientID:         "pat-1",
		PatientFullName:   viewPatientName,
		PrescriptionID:    viewPrescID,
		TotalSessions:     10,
		RemainingSessions: 4,
	}}
}

func TestPrescriptionDashboardCopyFollowsTheLocale(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		locale  i18n.Locale
		title   string
		create  string
		confirm string
	}{
		{"spanish", i18n.LocaleES, viewTitleES, viewCreateES, viewConfirmES},
		{"english", i18n.LocaleEN, viewTitleEN, viewCreateEN, viewConfirmEN},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body := i18ntest.Render(t, tt.locale, prescription.Dashboard(sampleBalances()))

			assert.Contains(t, body, tt.title)
			assert.Contains(t, body, tt.create)
			assert.Contains(t, body, tt.confirm)
			assert.Contains(t, body, viewPatientName, "data must survive translation")
			assert.Contains(t, body, ">10<", "the session counts must survive translation")
			assert.Contains(t, body, ">4<")
		})
	}
}

func TestPrescriptionEmptyStateFollowsTheLocale(t *testing.T) {
	t.Parallel()

	assert.Contains(t, i18ntest.Render(t, i18n.LocaleES, prescription.Dashboard(nil)), viewEmptyES)
	assert.Contains(t, i18ntest.Render(t, i18n.LocaleEN, prescription.Dashboard(nil)), viewEmptyEN)
}

func TestPrescriptionFormCopyFollowsTheLocale(t *testing.T) {
	t.Parallel()

	patients := []prescription.PatientOption{{ID: "pat-1", Label: viewPatientName}}

	form := func(locale i18n.Locale) string {
		return i18ntest.Render(t, locale, prescription.Form(patients, "/prescriptions"))
	}

	assert.Contains(t, form(i18n.LocaleES), viewDocES)
	assert.Contains(t, form(i18n.LocaleEN), viewDocEN)
	assert.Contains(t, form(i18n.LocaleES), viewPatientName, "option labels must survive translation")
}
