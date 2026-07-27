package appointment

type FormRequest struct {
	SlotID         string
	PatientID      string
	ProfessionalID string
	Notes          string
}

type SlotOptionDTO struct {
	ID               string
	FallbackLabel    string
	StartTime        string
	EndTime          string
	ProfessionalName string
	ProfessionalID   string
}

type PatientOptionDTO struct {
	ID                string
	Label             string
	RemainingSessions int
}

type ProfessionalOptionDTO struct {
	ID    string
	Label string
}
