package model

import (
	"time"

	"gorm.io/gorm"
)

// ImpactAssessment models an immutable snapshot of the 影响评估 performed when a
// deviation enters the evaluated (decided) state. Every re-evaluation creates a
// new AssessmentVersion for the same excursion; only the latest row is "current"
// and all earlier rows become "superseded" when the deviation is sent back for
// re-review. Disposition decisions reference a specific assessment version.
type ImpactAssessment struct {
	ID                  uint           `json:"id" gorm:"primaryKey"`
	ExcursionCode       string         `json:"excursionCode" gorm:"size:64;not null;index:idx_assessment_excursion_version,unique,priority:1"`
	AssessmentVersion   uint           `json:"assessmentVersion" gorm:"not null;index:idx_assessment_excursion_version,unique,priority:2"`
	Summary             string         `json:"summary" gorm:"size:1000"`
	ImpactLevel         string         `json:"impactLevel" gorm:"size:32;index"`
	AffectedProduct     string         `json:"affectedProduct" gorm:"size:160"`
	StabilityConclusion string         `json:"stabilityConclusion" gorm:"size:1000"`
	RiskLevel           string         `json:"riskLevel" gorm:"size:32;index"`
	ObservedTempC       float64        `json:"observedTempC"`
	DurationMinutes     int            `json:"durationMinutes"`
	SensorEvidence      string         `json:"sensorEvidence" gorm:"size:2000"`
	EvaluatedBy         string         `json:"evaluatedBy" gorm:"size:80;index"`
	EvaluatedAt         time.Time      `json:"evaluatedAt" gorm:"index"`
	Status              string         `json:"status" gorm:"size:32;index;not null"`
	SupersededBy        string         `json:"supersededBy" gorm:"size:80"`
	SupersededAt        *time.Time     `json:"supersededAt"`
	SupersedeReason     string         `json:"supersedeReason" gorm:"size:500"`
	CreatedAt           time.Time      `json:"createdAt"`
	UpdatedAt           time.Time      `json:"updatedAt"`
	DeletedAt           gorm.DeletedAt `json:"-" gorm:"index"`
}

func (item ImpactAssessment) TableName() string { return "impact_assessments" }
