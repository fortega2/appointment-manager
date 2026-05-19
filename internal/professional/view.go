package professional

import "fmt"

type View struct {
	ID        string
	FirstName string
	LastName  string
	Phone     string
	Specialty string
	Active    bool
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
