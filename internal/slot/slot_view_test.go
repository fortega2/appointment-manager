package slot_test

import (
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"appointment-manager/internal/i18n"
	"appointment-manager/internal/slot"
)

const (
	viewSlotID     = "11111111-2222-3333-4444-555555555555"
	viewProfName   = "Ada Lovelace"
	viewEmptyES    = "No hay horarios"
	viewEmptyEN    = "There are no slots"
	viewCreateES   = "Crear horario"
	viewCreateEN   = "Create Slot"
	viewStatusES   = "Disponible"
	viewStatusEN   = "Available"
	viewConfirmES  = "¿Cancelar este horario?"
	viewConfirmEN  = "Cancel this slot?"
	viewCapacityES = "Cupo máximo"
	viewCapacityEN = "Max Capacity"
)

func renderSlot(t *testing.T, locale i18n.Locale, component templ.Component) string {
	t.Helper()

	var body strings.Builder
	require.NoError(t, component.Render(i18n.WithLocale(t.Context(), locale), &body))

	return body.String()
}

func sampleSlots() []slot.ListDTO {
	start := time.Date(2026, time.May, 25, 10, 0, 0, 0, time.UTC)

	return []slot.ListDTO{{
		ID:               viewSlotID,
		ProfessionalID:   professionalID,
		ProfessionalName: viewProfName,
		StartTime:        start,
		EndTime:          start.Add(time.Hour),
		MaxCapacity:      slotMaxCapacity,
	}}
}

func TestDashboardCopyFollowsTheLocale(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		locale  i18n.Locale
		create  string
		status  string
		confirm string
	}{
		{"spanish", i18n.LocaleES, viewCreateES, viewStatusES, viewConfirmES},
		{"english", i18n.LocaleEN, viewCreateEN, viewStatusEN, viewConfirmEN},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body := renderSlot(t, tt.locale, slot.Dashboard(sampleSlots(), nil))

			assert.Contains(t, body, tt.create)
			assert.Contains(t, body, tt.status)
			assert.Contains(t, body, tt.confirm)
			assert.Contains(t, body, viewProfName, "data must survive translation")
		})
	}
}

func TestDashboardEmptyStateFollowsTheLocale(t *testing.T) {
	t.Parallel()

	assert.Contains(t, renderSlot(t, i18n.LocaleES, slot.Dashboard(nil, nil)), viewEmptyES)
	assert.Contains(t, renderSlot(t, i18n.LocaleEN, slot.Dashboard(nil, nil)), viewEmptyEN)
}

func TestFormCopyFollowsTheLocale(t *testing.T) {
	t.Parallel()

	empty := slot.ListDTO{}

	assert.Contains(t, renderSlot(t, i18n.LocaleES, slot.Form(empty, "/slots", nil)), viewCapacityES)
	assert.Contains(t, renderSlot(t, i18n.LocaleEN, slot.Form(empty, "/slots", nil)), viewCapacityEN)
}
