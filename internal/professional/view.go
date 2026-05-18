package professional

type View struct {
	ID        string
	FirstName string
	LastName  string
	Phone     string
	Specialty string
	Active    bool
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
