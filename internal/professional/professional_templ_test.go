package professional_test

import (
	"bytes"
	"testing"

	"appointment-manager/internal/i18n"
	"appointment-manager/internal/professional"
	"appointment-manager/internal/ui/form"

	"github.com/a-h/templ"
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

	titleES     = "<title>Profesionales"
	titleEN     = "<title>Professional Dashboard"
	emptyES     = "No hay profesionales"
	emptyEN     = "There are no professionals"
	emptyBodyES = "Agregá un profesional"
	emptyBodyEN = "Add a professional to start"
	// Badges are matched with their delimiters: "Inactivo" contains "activo",
	// so a bare substring check cannot tell the two states apart.
	activeBadgeES   = ">Activo<"
	activeBadgeEN   = ">Active<"
	inactiveBadgeES = ">Inactivo<"
	inactiveBadgeEN = ">Inactive<"
	formCreateES    = "Crear profesional"
	formCreateEN    = "Create Professional"
	formEditES      = "Editar profesional"
	formEditEN      = "Edit Professional"
)

// renderIn renders a component as the given locale would see it.
func renderIn(t *testing.T, locale i18n.Locale, component templ.Component) string {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, component.Render(i18n.WithLocale(t.Context(), locale), &buf))

	return buf.String()
}

type localeCase struct {
	name          string
	locale        i18n.Locale
	title         string
	empty         string
	emptyBody     string
	activeBadge   string
	inactiveBadge string
	formCreate    string
	formEdit      string
}

func localeCases() []localeCase {
	return []localeCase{
		{
			name: "spanish", locale: i18n.LocaleES,
			title: titleES, empty: emptyES, emptyBody: emptyBodyES,
			activeBadge: activeBadgeES, inactiveBadge: inactiveBadgeES,
			formCreate: formCreateES, formEdit: formEditES,
		},
		{
			name: "english", locale: i18n.LocaleEN,
			title: titleEN, empty: emptyEN, emptyBody: emptyBodyEN,
			activeBadge: activeBadgeEN, inactiveBadge: inactiveBadgeEN,
			formCreate: formCreateEN, formEdit: formEditEN,
		},
	}
}

func TestProfessionalDashboard(t *testing.T) {
	t.Parallel()

	for _, tt := range localeCases() {
		t.Run(professionalCaseRenderDashboardEmpty+" in "+tt.name, func(t *testing.T) {
			t.Parallel()

			output := renderIn(t, tt.locale, professional.Dashboard(nil))

			assert.Contains(t, output, tt.title)
			assert.Contains(t, output, tt.empty)
			assert.Contains(t, output, tt.emptyBody)
		})

		t.Run(professionalCaseRenderDashboardPopulated+" in "+tt.name, func(t *testing.T) {
			t.Parallel()

			professionals := []professional.View{
				{FirstName: "Dr. Smith", Active: true},
			}

			output := renderIn(t, tt.locale, professional.Dashboard(professionals))

			assert.Contains(t, output, tt.title)
			assert.Contains(t, output, "Dr. Smith")
			assert.NotContains(t, output, tt.empty)
		})
	}
}

func TestProfessionalTable(t *testing.T) {
	t.Parallel()

	for _, tt := range localeCases() {
		t.Run(professionalCaseRenderTableActive+" in "+tt.name, func(t *testing.T) {
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

			output := renderIn(t, tt.locale, professional.Table(professionals))

			assert.Contains(t, output, "Gregory")
			assert.Contains(t, output, "House")
			assert.Contains(t, output, "Diagnostician")
			assert.Contains(t, output, tt.activeBadge)
			assert.NotContains(t, output, tt.inactiveBadge)
			assert.Contains(t, output, `hx-get="/professionals/pro-1/edit"`)
		})

		t.Run(professionalCaseRenderTableInactive+" in "+tt.name, func(t *testing.T) {
			t.Parallel()

			professionals := []professional.View{
				{
					ID:        "pro-2",
					FirstName: "John",
					LastName:  "Watson",
					Active:    false,
				},
			}

			output := renderIn(t, tt.locale, professional.Table(professionals))

			assert.Contains(t, output, "John")
			assert.Contains(t, output, "Watson")
			assert.Contains(t, output, tt.inactiveBadge)
			assert.NotContains(t, output, tt.activeBadge)
		})
	}
}

func TestProfessionalForm(t *testing.T) {
	t.Parallel()

	for _, tt := range localeCases() {
		t.Run(professionalCaseRenderFormCreate+" in "+tt.name, func(t *testing.T) {
			t.Parallel()

			output := renderIn(t, tt.locale, professional.Form(professional.View{}, form.MethodPost, "/professionals"))

			assert.Contains(t, output, tt.formCreate)
			assert.Contains(t, output, `hx-post="/professionals"`)
			// Active checkbox should NOT be in create form
			assert.NotContains(t, output, `name="active"`)
		})

		t.Run(professionalCaseRenderFormEdit+" in "+tt.name, func(t *testing.T) {
			t.Parallel()

			p := professional.View{
				FirstName: "Lisa",
				LastName:  "Cuddy",
				Active:    true,
			}

			output := renderIn(t, tt.locale, professional.Form(p, form.MethodPut, "/professionals/pro-1"))

			assert.Contains(t, output, tt.formEdit)
			assert.Contains(t, output, `hx-put="/professionals/pro-1"`)
			assert.Contains(t, output, `value="Lisa"`)
			assert.Contains(t, output, `value="Cuddy"`)
			// Active checkbox SHOULD be in edit form and checked
			assert.Contains(t, output, `name="active"`)
			assert.Contains(t, output, `checked`)
		})
	}
}
