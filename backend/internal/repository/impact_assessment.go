package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blueship581/clinical-coldchain-deviation-control/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrStaleAssessment indicates a decision references an assessment version that
// is no longer the current one for its deviation (it has been superseded).
var ErrStaleAssessment = errors.New("decision references an assessment version that is no longer current")

// CompleteAssessmentInput carries the aggregates written atomically when a
// deviation completes an impact evaluation: a new assessment version is
// inserted and the deviation points at it.
type CompleteAssessmentInput struct {
	ExcursionID              uint
	Assessment               *model.ImpactAssessment
	ExpectedAggregateVersion uint
	Audit                    *model.AuditLog
}

// ReturnForReviewInput invalidates all decisions bound to the version being
// superseded and moves the deviation back to review inside one transaction.
type ReturnForReviewInput struct {
	ExcursionID              uint
	ExpectedAggregateVersion uint
	Reason                   string
	Actor                    string
	Audit                    *model.AuditLog
	DecisionAuditAction      string
	RequestID                string
}

// ApproveFinalDecisionInput independently approves a disposition while
// asserting, under a lock on the deviation, that its assessment version is
// still current.
type ApproveFinalDecisionInput struct {
	DecisionID              uint
	ExpectedDecisionVersion uint
	TargetStatus            string
	Evidence                string
	Reason                  string
	Actor                   string
	Audit                   *model.AuditLog
}

// CreateProposalInput inserts a disposition draft bound to the deviation's
// current assessment version, which is re-checked under a row lock.
type CreateProposalInput struct {
	Decision *model.DispositionDecision
	Audit    *model.AuditLog
}

// ImpactAssessmentRepository persists assessment versions and also hosts the
// cross-aggregate workflow transactions that keep deviations, assessments and
// dispositions consistent under concurrent review/approval requests.
type ImpactAssessmentRepository interface {
	ListForExcursion(ctx context.Context, excursionCode string) ([]model.ImpactAssessment, error)
	LatestForExcursion(ctx context.Context, excursionCode string) (model.ImpactAssessment, error)
	Create(ctx context.Context, item *model.ImpactAssessment) error
	CompleteAssessment(ctx context.Context, input CompleteAssessmentInput) (model.ExcursionEvent, model.ImpactAssessment, error)
	ReturnForReview(ctx context.Context, input ReturnForReviewInput) (model.ExcursionEvent, []model.DispositionDecision, error)
	ApproveFinalDecision(ctx context.Context, input ApproveFinalDecisionInput) (model.DispositionDecision, model.ExcursionEvent, error)
	CreateProposal(ctx context.Context, input CreateProposalInput) (model.DispositionDecision, model.ExcursionEvent, error)
}

type impactAssessmentRepository struct{ db *gorm.DB }

func NewImpactAssessmentRepository(db *gorm.DB) ImpactAssessmentRepository {
	return &impactAssessmentRepository{db: db}
}

func (r *impactAssessmentRepository) ListForExcursion(ctx context.Context, excursionCode string) ([]model.ImpactAssessment, error) {
	items := make([]model.ImpactAssessment, 0)
	err := r.db.WithContext(ctx).Where("excursion_code = ?", excursionCode).
		Order("assessment_version DESC, id DESC").Find(&items).Error
	return items, err
}

func (r *impactAssessmentRepository) LatestForExcursion(ctx context.Context, excursionCode string) (model.ImpactAssessment, error) {
	var item model.ImpactAssessment
	err := r.db.WithContext(ctx).Where("excursion_code = ?", excursionCode).
		Order("assessment_version DESC, id DESC").First(&item).Error
	return item, err
}

func (r *impactAssessmentRepository) Create(ctx context.Context, item *model.ImpactAssessment) error {
	return r.db.WithContext(ctx).Create(item).Error
}

// forUpdate locks the referenced row for the duration of the transaction on
// row-locking databases. SQLite serialises writes and ignores the clause.
func forUpdate(tx *gorm.DB) *gorm.DB {
	if tx.Dialector.Name() == "sqlite" {
		return tx
	}
	return tx.Clauses(clause.Locking{Strength: "UPDATE"})
}

func (r *impactAssessmentRepository) CompleteAssessment(ctx context.Context, input CompleteAssessmentInput) (model.ExcursionEvent, model.ImpactAssessment, error) {
	var excursion model.ExcursionEvent
	var assessment model.ImpactAssessment
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := forUpdate(tx).First(&excursion, input.ExcursionID).Error; err != nil {
			return err
		}
		if excursion.Version != input.ExpectedAggregateVersion {
			return ErrVersionConflict
		}
		// The next version follows the highest version ever produced for the
		// deviation, so a re-evaluation after return-for-review always gets a
		// fresh number even though the current pointer was cleared.
		var maxVersion uint
		if err := tx.Model(&model.ImpactAssessment{}).
			Where("excursion_code = ?", excursion.Code).
			Select("COALESCE(MAX(assessment_version), 0)").Scan(&maxVersion).Error; err != nil {
			return err
		}
		nextVersion := maxVersion + 1
		input.Assessment.ExcursionCode = excursion.Code
		input.Assessment.AssessmentVersion = nextVersion
		now := time.Now().UTC()
		result := tx.Model(&model.ExcursionEvent{}).
			Where("id = ? AND version = ?", input.ExcursionID, input.ExpectedAggregateVersion).
			Updates(map[string]any{
				"status":                     "decided",
				"reviewer":                   input.Assessment.EvaluatedBy,
				"sensor_evidence":            input.Assessment.SensorEvidence,
				"evidence":                   input.Assessment.SensorEvidence,
				"current_assessment_version": nextVersion,
				"updated_at":                 now,
				"version":                    gorm.Expr("version + 1"),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrVersionConflict
		}
		input.Assessment.CreatedAt = now
		input.Assessment.UpdatedAt = now
		if err := tx.Create(input.Assessment).Error; err != nil {
			return err
		}
		if input.Audit != nil {
			if err := tx.Create(input.Audit).Error; err != nil {
				return err
			}
		}
		if err := forUpdate(tx).First(&excursion, input.ExcursionID).Error; err != nil {
			return err
		}
		assessment = *input.Assessment
		return nil
	})
	return excursion, assessment, err
}

func (r *impactAssessmentRepository) ReturnForReview(ctx context.Context, input ReturnForReviewInput) (model.ExcursionEvent, []model.DispositionDecision, error) {
	var excursion model.ExcursionEvent
	invalidated := make([]model.DispositionDecision, 0)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := forUpdate(tx).First(&excursion, input.ExcursionID).Error; err != nil {
			return err
		}
		if excursion.Version != input.ExpectedAggregateVersion {
			return ErrVersionConflict
		}
		if excursion.Status != "decided" || excursion.CurrentAssessmentVersion == nil {
			return fmt.Errorf("%w: only an evaluated deviation can be sent back for re-review", ErrInvalidWorkflowState)
		}
		version := *excursion.CurrentAssessmentVersion
		// The assessment version becomes history; decisions bound to it can never
		// be approved or close the deviation again.
		now := time.Now().UTC()
		if err := tx.Model(&model.ImpactAssessment{}).
			Where("excursion_code = ? AND assessment_version = ?", excursion.Code, version).
			Updates(map[string]any{
				"status":           "superseded",
				"superseded_by":    input.Actor,
				"superseded_at":    now,
				"supersede_reason": input.Reason,
				"updated_at":       now,
			}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.DispositionDecision{}).
			Where("excursion_code = ? AND assessment_version = ? AND invalidated_reason = ''", excursion.Code, version).
			Find(&invalidated).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.DispositionDecision{}).
			Where("excursion_code = ? AND assessment_version = ? AND invalidated_reason = ''", excursion.Code, version).
			Updates(map[string]any{
				"invalidated_reason": input.Reason,
				"invalidated_at":     now,
				"updated_at":         now,
			}).Error; err != nil {
			return err
		}
		result := tx.Model(&model.ExcursionEvent{}).
			Where("id = ? AND version = ?", input.ExcursionID, input.ExpectedAggregateVersion).
			Updates(map[string]any{
				"status":                     "in_review",
				"reviewer":                   input.Actor,
				"current_assessment_version": nil,
				"updated_at":                 now,
				"version":                    gorm.Expr("version + 1"),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrVersionConflict
		}
		if input.Audit != nil {
			if err := tx.Create(input.Audit).Error; err != nil {
				return err
			}
		}
		// Each invalidated decision gets its own audit row so the decision's
		// entity history keeps the before/after reason available.
		for i := range invalidated {
			detail := fmt.Sprintf(`{"reason":%q,"excursionCode":%q,"assessmentVersion":%d}`, input.Reason, excursion.Code, version)
			audit := &model.AuditLog{
				RequestID: input.RequestID, Actor: input.Actor,
				Action: input.DecisionAuditAction, EntityType: "DispositionDecision",
				EntityID: invalidated[i].ID, BeforeState: invalidated[i].Status,
				AfterState: invalidated[i].Status + ":invalidated", Detail: detail, CreatedAt: now,
			}
			if err := tx.Create(audit).Error; err != nil {
				return err
			}
		}
		if err := forUpdate(tx).First(&excursion, input.ExcursionID).Error; err != nil {
			return err
		}
		return nil
	})
	return excursion, invalidated, err
}

func (r *impactAssessmentRepository) ApproveFinalDecision(ctx context.Context, input ApproveFinalDecisionInput) (model.DispositionDecision, model.ExcursionEvent, error) {
	var decision model.DispositionDecision
	var excursion model.ExcursionEvent
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&decision, input.DecisionID).Error; err != nil {
			return err
		}
		if decision.Status != "draft" {
			return fmt.Errorf("%w: only a draft disposition can be approved", ErrInvalidWorkflowState)
		}
		if strings.TrimSpace(decision.InvalidatedReason) != "" {
			return fmt.Errorf("%w: %s", ErrStaleAssessment, decision.InvalidatedReason)
		}
		if err := forUpdate(tx).Where("code = ?", decision.ExcursionCode).First(&excursion).Error; err != nil {
			return err
		}
		if excursion.Status != "decided" ||
			excursion.CurrentAssessmentVersion == nil ||
			decision.AssessmentVersion == nil ||
			*decision.AssessmentVersion != *excursion.CurrentAssessmentVersion {
			return ErrStaleAssessment
		}
		now := time.Now().UTC()
		result := tx.Model(&model.DispositionDecision{}).
			Where("id = ? AND version = ? AND status = 'draft' AND invalidated_reason = ''", input.DecisionID, input.ExpectedDecisionVersion).
			Updates(map[string]any{
				"status":          input.TargetStatus,
				"sensor_evidence": input.Evidence,
				"evidence":        input.Evidence,
				"decision_basis":  input.Reason,
				"approved_by":     input.Actor,
				"decided_at":      now,
				"updated_at":      now,
				"version":         gorm.Expr("version + 1"),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrVersionConflict
		}
		if input.Audit != nil {
			if err := tx.Create(input.Audit).Error; err != nil {
				return err
			}
		}
		if err := tx.First(&decision, input.DecisionID).Error; err != nil {
			return err
		}
		return nil
	})
	return decision, excursion, err
}

func (r *impactAssessmentRepository) CreateProposal(ctx context.Context, input CreateProposalInput) (model.DispositionDecision, model.ExcursionEvent, error) {
	var decision model.DispositionDecision
	var excursion model.ExcursionEvent
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := forUpdate(tx).Where("code = ?", input.Decision.ExcursionCode).First(&excursion).Error; err != nil {
			return err
		}
		if excursion.Status != "decided" || excursion.CurrentAssessmentVersion == nil {
			return fmt.Errorf("%w: the deviation must have a current impact assessment before a disposition is proposed", ErrStaleAssessment)
		}
		input.Decision.AssessmentVersion = excursion.CurrentAssessmentVersion
		if err := tx.Create(input.Decision).Error; err != nil {
			return err
		}
		if input.Audit != nil {
			input.Audit.EntityID = input.Decision.ID
			if err := tx.Create(input.Audit).Error; err != nil {
				return err
			}
		}
		decision = *input.Decision
		return nil
	})
	return decision, excursion, err
}
