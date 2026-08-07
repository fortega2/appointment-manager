package professional

import (
	"context"
	"fmt"

	"appointment-manager/internal/i18n"
)

type View struct {
	ID        string
	FirstName string
	LastName  string
	Phone     string
	Specialty string
	Active    bool
}

// SpecialtyLabel translates the stored specialty, falling back to the raw value.
func (v View) SpecialtyLabel(ctx context.Context) string {
	key, ok := specialtyLabelKey(v.Specialty)
	if !ok {
		return v.Specialty
	}

	return i18n.T(ctx, key)
}

func (v View) AlpineVisibility() string {
	return fmt.Sprintf(`(statusFilter === 'all' || statusFilter === '%t') && (searchQuery === '' || $el.dataset.search.toLowerCase().includes(searchQuery.toLowerCase()))`, v.Active)
}

func professionalToView(p *Professional) View {
	return View{
		ID:        p.ID.String(),
		FirstName: p.FirstName,
		LastName:  p.LastName,
		Phone:     p.Phone,
		Specialty: p.Specialty,
		Active:    p.Active,
	}
}

func professionalsToViews(professionals []Professional) []View {
	views := make([]View, len(professionals))
	for i, p := range professionals {
		views[i] = View{
			ID:        p.ID.String(),
			FirstName: p.FirstName,
			LastName:  p.LastName,
			Phone:     p.Phone,
			Specialty: p.Specialty,
			Active:    p.Active,
		}
	}
	return views
}
