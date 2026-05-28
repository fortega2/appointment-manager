package slot

type ListDTO struct {
	ID               string
	ProfessionalID   string
	ProfessionalName string
	Date             string
	StartTime        string
	EndTime          string
	MaxCapacity      int16
	Blocked          bool
}
