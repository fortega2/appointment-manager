package professional_test

import (
	"bytes"
	"testing"

	"appointment-manager/internal/professional"
	"appointment-manager/internal/ui/form"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	professionalCaseRenderDashboardEmpty     = "render dashboard empty"
	professionalCaseRenderDashboardPopulated = "render dashboard populated"
	professionalCaseRenderTableActive        = "render table active professional"
	professionalCaseRenderTableInactive      = "render table inactive professional"
	professionalCaseRenderFormCreate         = "render form create"
	professionalCaseRenderFormEdit           = "render form edit"
)

func TestProfessionalDashboard(t *testing.T) {
	t.Parallel()

	t.Run(professionalCaseRenderDashboardEmpty, func(t *testing.T) {
		t.Parallel()

		component := professional.Dashboard(nil)

		var buf bytes.Buffer
		err := component.Render(t.Context(), &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "Professional Dashboard")
		assert.Contains(t, output, "There are no professionals")
		assert.Contains(t, output, "Add a professional to start")
	})

	t.Run(professionalCaseRenderDashboardPopulated, func(t *testing.T) {
		t.Parallel()

		professionals := []professional.View{
			{FirstName: "Dr. Smith", Active: true},
		}
		component := professional.Dashboard(professionals)

		var buf bytes.Buffer
		err := component.Render(t.Context(), &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "Professional Dashboard")
		assert.Contains(t, output, "Dr. Smith")
		assert.NotContains(t, output, "There are no professionals")
	})
}

func TestProfessionalTable(t *testing.T) {
	t.Parallel()

	t.Run(professionalCaseRenderTableActive, func(t *testing.T) {
		t.Parallel()

		professionals := []professional.View{
			{
				ID:        "pro-1",
				FirstName: "Gregory",
				LastName:  "House",
				Phone:     "123-456",
				Specialty: "Diagnostician",
				Active:    true,
			},
		}

		component := professional.Table(professionals)

		var buf bytes.Buffer
		err := component.Render(t.Context(), &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "Gregory")
		assert.Contains(t, output, "House")
		assert.Contains(t, output, "Diagnostician")
		assert.Contains(t, output, "Active")
		assert.NotContains(t, output, ">Inactive<") // text of inactive badge
		assert.Contains(t, output, `hx-get="/professionals/pro-1/edit"`)
	})

	t.Run(professionalCaseRenderTableInactive, func(t *testing.T) {
		t.Parallel()

		professionals := []professional.View{
			{
				ID:        "pro-2",
				FirstName: "John",
				LastName:  "Watson",
				Active:    false,
			},
		}

		component := professional.Table(professionals)

		var buf bytes.Buffer
		err := component.Render(t.Context(), &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "John")
		assert.Contains(t, output, "Watson")
		assert.Contains(t, output, "Inactive")
	})
}

func TestProfessionalForm(t *testing.T) {
	t.Parallel()

	t.Run(professionalCaseRenderFormCreate, func(t *testing.T) {
		t.Parallel()

		p := professional.View{}
		component := professional.Form(p, form.MethodPost, "/professionals")

		var buf bytes.Buffer
		err := component.Render(t.Context(), &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "Create Professional")
		assert.Contains(t, output, `hx-post="/professionals"`)
		// Active checkbox should NOT be in create form
		assert.NotContains(t, output, `name="active"`)
	})

	t.Run(professionalCaseRenderFormEdit, func(t *testing.T) {
		t.Parallel()

		p := professional.View{
			FirstName: "Lisa",
			LastName:  "Cuddy",
			Active:    true,
		}
		component := professional.Form(p, form.MethodPut, "/professionals/pro-1")

		var buf bytes.Buffer
		err := component.Render(t.Context(), &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "Edit Professional")
		assert.Contains(t, output, `hx-put="/professionals/pro-1"`)
		assert.Contains(t, output, `value="Lisa"`)
		assert.Contains(t, output, `value="Cuddy"`)
		// Active checkbox SHOULD be in edit form and checked
		assert.Contains(t, output, `name="active"`)
		assert.Contains(t, output, `checked`)
	})
}
