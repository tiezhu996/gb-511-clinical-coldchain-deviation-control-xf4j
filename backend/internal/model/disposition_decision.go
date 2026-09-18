package model

import "time"

// DispositionDecision models 处置决定 as an independently versioned aggregate. The fields
// cover ownership, operational context, evidence and measured risk so later
// changes naturally span persistence, service and UI layers.
type DispositionDecision struct {
	BaseModel
	ExcursionCode  string     `json:"excursionCode" gorm:"size:64;index"`
	DecisionBasis  string     `json:"decisionBasis" gorm:"size:1000"`
	SensorEvidence string     `json:"sensorEvidence" gorm:"size:2000"`
	ProposedBy     string     `json:"proposedBy" gorm:"size:80;index"`
	ApprovedBy     string     `json:"approvedBy" gorm:"size:80;index"`
	DecidedAt      *time.Time `json:"decidedAt"`
	Facility       string     `json:"facility" gorm:"size:120;index"`
	Owner          string     `json:"owner" gorm:"size:120;index"`
	Category       string     `json:"category" gorm:"size:80;index"`
	RiskLevel      string     `json:"riskLevel" gorm:"size:32;index"`
	MetricValue    float64    `json:"metricValue"`
	MetricUnit     string     `json:"metricUnit" gorm:"size:24"`
	EffectiveAt    time.Time  `json:"effectiveAt"`
	Evidence       string     `json:"evidence" gorm:"size:2000"`
	RelatedCode    string     `json:"relatedCode" gorm:"size:64;index"`
	// AssessmentVersion pins the proposal to the ImpactAssessment version that
	// was current when it was raised. A decision can close the deviation only
	// while this still matches the deviation's current version.
	AssessmentVersion *uint `json:"assessmentVersion" gorm:"index"`
	// InvalidatedReason is set when the deviation is returned for re-review.
	// Such decisions stay visible as history but can never be approved or used
	// to close the deviation again.
	InvalidatedReason string     `json:"invalidatedReason" gorm:"size:500"`
	InvalidatedAt     *time.Time `json:"invalidatedAt"`
}

// IsFinal reports whether the disposition has reached an immutable release,
// quarantine or discard state.
func (item DispositionDecision) IsFinal() bool {
	switch item.Status {
	case "release", "quarantine", "discard":
		return true
	default:
		return false
	}
}

// IsEffectiveFor reports whether the decision can still drive closure of its
// deviation: it must be final, never invalidated, and it must match the
// assessment version currently effective for the deviation.
func (item DispositionDecision) IsEffectiveFor(currentAssessmentVersion *uint) bool {
	return item.IsFinal() &&
		item.InvalidatedReason == "" &&
		item.AssessmentVersion != nil && currentAssessmentVersion != nil &&
		*item.AssessmentVersion == *currentAssessmentVersion
}

func (item *DispositionDecision) GetBase() *BaseModel { return &item.BaseModel }

func (item DispositionDecision) TableName() string { return "disposition_decisions" }

var DispositionDecisionInitialStatus = "draft"
