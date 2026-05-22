package patient_test

import (
	"bytes"
	"testing"

	"appointment-manager/internal/healthinsurance"
	"appointment-manager/internal/patient"
	"appointment-manager/internal/ui/form"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	patientCaseRenderDashboardEmpty     = "render dashboard empty"
	patientCaseRenderDashboardPopulated = "render dashboard populated"
	patientCaseRenderTable              = "render table with patients"
	patientCaseRenderFormCreate         = "render form create"
	patientCaseRenderFormEdit           = "render form edit"
)

func TestPatientDashboard(t *testing.T) {
	t.Parallel()

	t.Run(patientCaseRenderDashboardEmpty, func(t *testing.T) {
		t.Parallel()

		component := patient.Dashboard(nil)

		var buf bytes.Buffer
		err := component.Render(t.Context(), &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "Patient Dashboard")
		assert.Contains(t, output, "There are no patients")
		assert.Contains(t, output, "Add a patient to start")
	})

	t.Run(patientCaseRenderDashboardPopulated, func(t *testing.T) {
		t.Parallel()

		patients := []patient.View{
			{FirstName: "John", LastName: "Doe"},
		}
		component := patient.Dashboard(patients)

		var buf bytes.Buffer
		err := component.Render(t.Context(), &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "Patient Dashboard")
		assert.Contains(t, output, "John")
		assert.NotContains(t, output, "There are no patients")
	})
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

		component := patient.Table(patients)

		var buf bytes.Buffer
		err := component.Render(t.Context(), &buf)
		require.NoError(t, err)

		output := buf.String()
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

	t.Run(patientCaseRenderFormCreate, func(t *testing.T) {
		t.Parallel()

		p := patient.View{}
		component := patient.Form(p, form.MethodPost, "/patients", insurances)

		var buf bytes.Buffer
		err := component.Render(t.Context(), &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "Create Patient")
		assert.Contains(t, output, `hx-post="/patients"`)
		assert.Contains(t, output, "OSDE")
		assert.Contains(t, output, "Swiss Medical")
	})

	t.Run(patientCaseRenderFormEdit, func(t *testing.T) {
		t.Parallel()

		p := patient.View{
			FirstName:       "Alice",
			HealthInsurance: 2,
			ClinicalNotes:   "Some notes",
		}
		component := patient.Form(p, form.MethodPut, "/patients/123", insurances)

		var buf bytes.Buffer
		err := component.Render(t.Context(), &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "Edit Patient")
		assert.Contains(t, output, `hx-put="/patients/123"`)
		assert.Contains(t, output, `value="Alice"`)
		assert.Contains(t, output, "Some notes")
		assert.Contains(t, output, `value="2" selected`) // Swiss Medical selected
	})
}
