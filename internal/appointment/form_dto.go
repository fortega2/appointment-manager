package appointment

type FormRequest struct {
	SlotID         string
	PatientID      string
	ProfessionalID string
	AssistantID    string
	Notes          string
}

type SlotOptionDTO struct {
	ID             string
	Label          string
	ProfessionalID string
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

type AssistantOptionDTO struct {
	ID    string
	Label string
}
