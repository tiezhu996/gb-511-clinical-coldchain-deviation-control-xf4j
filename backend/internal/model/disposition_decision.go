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
	// AssessmentVersion pins the decision to the excursion 影响评估 version it was
	// proposed against. Only decisions matching the excursion's current version may be
	// approved or used to close the excursion.
	AssessmentVersion uint `json:"assessmentVersion" gorm:"not null;default:0;index"`
	// InvalidatedAt/InvalidatedReason turn a stale decision into read-only history once
	// the excursion is returned for re-review; the row itself is never reused.
	InvalidatedAt     *time.Time `json:"invalidatedAt"`
	InvalidatedReason string     `json:"invalidatedReason" gorm:"size:200"`
}

func (item *DispositionDecision) GetBase() *BaseModel { return &item.BaseModel }

func (item DispositionDecision) TableName() string { return "disposition_decisions" }

var DispositionDecisionInitialStatus = "draft"
