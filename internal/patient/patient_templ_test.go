package patient_test

import (
	"testing"

	"appointment-manager/internal/healthinsurance"
	"appointment-manager/internal/i18n"
	"appointment-manager/internal/i18n/i18ntest"
	"appointment-manager/internal/patient"
	"appointment-manager/internal/ui/form"

	"github.com/stretchr/testify/assert"
)

const (
	patientCaseRenderDashboardEmpty     = "render dashboard empty"
	patientCaseRenderDashboardPopulated = "render dashboard populated"
	patientCaseRenderTable              = "render table with patients"
	patientCaseRenderFormCreate         = "render form create"
	patientCaseRenderFormEdit           = "render form edit"

	// Anchored to the tag: the nav link also says "Pacientes", so a bare
	// substring check would pass even with the title left untranslated.
	titleES      = "<title>Pacientes"
	titleEN      = "<title>Patients"
	emptyES      = "No hay pacientes"
	emptyEN      = "There are no patients"
	emptyBodyES  = "Agregá un paciente"
	emptyBodyEN  = "Add a patient to start"
	formCreateES = "Crear paciente"
	formCreateEN = "Create Patient"
	formEditES   = "Editar paciente"
	formEditEN   = "Edit Patient"
)

// renderIn renders a component as the given locale would see it.

func TestPatientDashboard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		locale    i18n.Locale
		title     string
		empty     string
		emptyBody string
	}{
		{"spanish", i18n.LocaleES, titleES, emptyES, emptyBodyES},
		{"english", i18n.LocaleEN, titleEN, emptyEN, emptyBodyEN},
	}

	for _, tt := range tests {
		t.Run(patientCaseRenderDashboardEmpty+" in "+tt.name, func(t *testing.T) {
			t.Parallel()

			output := i18ntest.Render(t, tt.locale, patient.Dashboard(nil))

			assert.Contains(t, output, tt.title)
			assert.Contains(t, output, tt.empty)
			assert.Contains(t, output, tt.emptyBody)
		})

		t.Run(patientCaseRenderDashboardPopulated+" in "+tt.name, func(t *testing.T) {
			t.Parallel()

			patients := []patient.View{
				{FirstName: "John", LastName: "Doe"},
			}

			output := i18ntest.Render(t, tt.locale, patient.Dashboard(patients))

			assert.Contains(t, output, tt.title)
			assert.Contains(t, output, "John")
			assert.NotContains(t, output, tt.empty)
		})
	}
}

func TestPatientTable(t *testing.T) {
	t.Parallel()

	t.Run(patientCaseRenderTable, func(t *testing.T) {
		t.Parallel()

		patients := []patient.View{
			{
				ID:                  "123",
				FirstName:           "Jane",
				LastName:            "Smith",
				Phone:               "555-1234",
				Email:               "jane@example.com",
				HealthInsuranceName: "OSDE",
			},
		}

		output := i18ntest.Render(t, i18n.LocaleES, patient.Table(patients))

		assert.Contains(t, output, "Jane")
		assert.Contains(t, output, "Smith")
		assert.Contains(t, output, "555-1234")
		assert.Contains(t, output, "jane@example.com")
		assert.Contains(t, output, "OSDE")
		assert.Contains(t, output, `hx-get="/patients/123/edit"`)
	})
}

func TestPatientForm(t *testing.T) {
	t.Parallel()

	insurances := []healthinsurance.HealthInsurance{
		{ID: 1, Name: "OSDE"},
		{ID: 2, Name: "Swiss Medical"},
	}

	tests := []struct {
		name   string
		locale i18n.Locale
		create string
		edit   string
	}{
		{"spanish", i18n.LocaleES, formCreateES, formEditES},
		{"english", i18n.LocaleEN, formCreateEN, formEditEN},
	}

	for _, tt := range tests {
		t.Run(patientCaseRenderFormCreate+" in "+tt.name, func(t *testing.T) {
			t.Parallel()

			output := i18ntest.Render(t, tt.locale, patient.Form(patient.View{}, form.MethodPost, "/patients", insurances))

			assert.Contains(t, output, tt.create)
			assert.Contains(t, output, `hx-post="/patients"`)
			assert.Contains(t, output, "OSDE")
			assert.Contains(t, output, "Swiss Medical")
		})

		t.Run(patientCaseRenderFormEdit+" in "+tt.name, func(t *testing.T) {
			t.Parallel()

			p := patient.View{
				FirstName:       "Alice",
				HealthInsurance: 2,
				ClinicalNotes:   "Some notes",
			}

			output := i18ntest.Render(t, tt.locale, patient.Form(p, form.MethodPut, "/patients/123", insurances))

			assert.Contains(t, output, tt.edit)
			assert.Contains(t, output, `hx-put="/patients/123"`)
			assert.Contains(t, output, `value="Alice"`)
			assert.Contains(t, output, "Some notes")
			assert.Contains(t, output, `value="2" selected`) // Swiss Medical selected
		})
	}
}
