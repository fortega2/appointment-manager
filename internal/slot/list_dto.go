package slot

import "time"

type ListDTO struct {
	ID               string
	ProfessionalID   string
	ProfessionalName string
	StartTime        time.Time
	EndTime          time.Time
	MaxCapacity      int16
	Blocked          bool
}
