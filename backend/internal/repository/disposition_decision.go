package repository

import (
	"context"

	"github.com/blueship581/clinical-coldchain-deviation-control/backend/internal/constants"
	"github.com/blueship581/clinical-coldchain-deviation-control/backend/internal/dto"
	"github.com/blueship581/clinical-coldchain-deviation-control/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DispositionDecisionRepository owns all persistence operations for 处置决定.
type DispositionDecisionRepository interface {
	List(context.Context, dto.PageQuery) (Page[model.DispositionDecision], error)
	Get(context.Context, uint) (model.DispositionDecision, error)
	Create(context.Context, *model.DispositionDecision) error
	CreateForCurrentAssessment(context.Context, *model.DispositionDecision) error
	Update(context.Context, uint, uint, *model.DispositionDecision, ...*model.AuditLog) error
	ApproveForCurrentAssessment(context.Context, *model.DispositionDecision, uint, ...*model.AuditLog) error
	Delete(context.Context, uint) error
	CountByStatus(context.Context) (map[string]int64, error)
	HasValidFinalForAssessment(context.Context, string, uint) (bool, error)
}

type dispositionDecisionRepository struct {
	store *Store[model.DispositionDecision]
}

func NewDispositionDecisionRepository(db *gorm.DB) DispositionDecisionRepository {
	return &dispositionDecisionRepository{store: NewStore[model.DispositionDecision](db)}
}

func (r *dispositionDecisionRepository) List(ctx context.Context, q dto.PageQuery) (Page[model.DispositionDecision], error) {
	return r.store.List(ctx, q)
}
func (r *dispositionDecisionRepository) Get(ctx context.Context, id uint) (model.DispositionDecision, error) {
	return r.store.Get(ctx, id)
}
func (r *dispositionDecisionRepository) Create(ctx context.Context, item *model.DispositionDecision) error {
	return r.store.Create(ctx, item)
}

// lockExcursion reads the excursion row under a row lock so a concurrent
// return-for-review cannot change its assessment state mid-transaction.
func lockExcursion(tx *gorm.DB, code string) (model.ExcursionEvent, error) {
	var excursion model.ExcursionEvent
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("code = ?", code).First(&excursion).Error
	return excursion, err
}

// CreateForCurrentAssessment inserts a proposal pinned to the excursion's current
// assessment version. The excursion must be in the decided (已评估) state; the lock
// guarantees a concurrent return-for-review either invalidates the new row or blocks it.
func (r *dispositionDecisionRepository) CreateForCurrentAssessment(ctx context.Context, item *model.DispositionDecision) error {
	return r.store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		excursion, err := lockExcursion(tx, item.ExcursionCode)
		if err != nil {
			return err
		}
		if excursion.Status != string(constants.ExcursionStateDecided) {
			return ErrAssessmentStale
		}
		item.AssessmentVersion = excursion.AssessmentVersion
		return tx.Create(item).Error
	})
}
func (r *dispositionDecisionRepository) Update(ctx context.Context, id, version uint, item *model.DispositionDecision, audits ...*model.AuditLog) error {
	return r.store.Update(ctx, id, version, item, audits...)
}

// ApproveForCurrentAssessment finalizes a draft decision only while it still matches the
// excursion's current assessment version. The excursion row lock serializes against a
// concurrent return-for-review so exactly one of the two operations keeps a valid result.
func (r *dispositionDecisionRepository) ApproveForCurrentAssessment(ctx context.Context, item *model.DispositionDecision, expectedVersion uint, audits ...*model.AuditLog) error {
	return r.store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		excursion, err := lockExcursion(tx, item.ExcursionCode)
		if err != nil {
			return err
		}
		if excursion.Status != string(constants.ExcursionStateDecided) || excursion.AssessmentVersion != item.AssessmentVersion {
			return ErrAssessmentStale
		}
		result := tx.Model(&model.DispositionDecision{}).Where("id = ? AND version = ?", item.ID, expectedVersion).
			Select("*").Omit("id", "code", "created_at", "deleted_at").Updates(item)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrVersionConflict
		}
		for _, audit := range audits {
			if audit != nil {
				if err := tx.Create(audit).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}
func (r *dispositionDecisionRepository) Delete(ctx context.Context, id uint) error {
	return r.store.Delete(ctx, id)
}
func (r *dispositionDecisionRepository) CountByStatus(ctx context.Context) (map[string]int64, error) {
	return r.store.CountByStatus(ctx)
}

// HasValidFinalForAssessment reports whether the excursion has a usable final decision
// for the given assessment version: not invalidated and approved by someone other than
// the proposer (双人复核).
func (r *dispositionDecisionRepository) HasValidFinalForAssessment(ctx context.Context, code string, assessmentVersion uint) (bool, error) {
	var count int64
	err := r.store.db.WithContext(ctx).Model(&model.DispositionDecision{}).
		Where("excursion_code = ? AND assessment_version = ? AND invalidated_at IS NULL", code, assessmentVersion).
		Where("status IN ?", []string{"release", "quarantine", "discard"}).
		Where("approved_by <> '' AND LOWER(approved_by) <> LOWER(proposed_by)").
		Count(&count).Error
	return count > 0, err
}
