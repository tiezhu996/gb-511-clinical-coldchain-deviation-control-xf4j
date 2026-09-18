package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/blueship581/clinical-coldchain-deviation-control/backend/internal/dto"
	"github.com/blueship581/clinical-coldchain-deviation-control/backend/internal/model"
	"gorm.io/gorm"
)

// ExcursionEventRepository owns all persistence operations for 偏差事件.
type ExcursionEventRepository interface {
	List(context.Context, dto.PageQuery) (Page[model.ExcursionEvent], error)
	Get(context.Context, uint) (model.ExcursionEvent, error)
	Create(context.Context, *model.ExcursionEvent) error
	Update(context.Context, uint, uint, *model.ExcursionEvent, ...*model.AuditLog) error
	ReturnForReview(context.Context, *model.ExcursionEvent, uint, string, time.Time, string, string, *model.AuditLog) (int64, error)
	Delete(context.Context, uint) error
	CountByStatus(context.Context) (map[string]int64, error)
}

type excursionEventRepository struct {
	store *Store[model.ExcursionEvent]
}

func NewExcursionEventRepository(db *gorm.DB) ExcursionEventRepository {
	return &excursionEventRepository{store: NewStore[model.ExcursionEvent](db)}
}

func (r *excursionEventRepository) List(ctx context.Context, q dto.PageQuery) (Page[model.ExcursionEvent], error) {
	return r.store.List(ctx, q)
}
func (r *excursionEventRepository) Get(ctx context.Context, id uint) (model.ExcursionEvent, error) {
	return r.store.Get(ctx, id)
}
func (r *excursionEventRepository) Create(ctx context.Context, item *model.ExcursionEvent) error {
	return r.store.Create(ctx, item)
}
func (r *excursionEventRepository) Update(ctx context.Context, id, version uint, item *model.ExcursionEvent, audits ...*model.AuditLog) error {
	return r.store.Update(ctx, id, version, item, audits...)
}

// ReturnForReview moves a decided excursion back to in_review and, in the same
// transaction, invalidates every disposition decision that still references the current
// assessment version. The excursion row is written first so concurrent decision
// approvals (which lock the excursion row) serialize after it and see the new state.
func (r *excursionEventRepository) ReturnForReview(ctx context.Context, item *model.ExcursionEvent, expectedVersion uint, reason string, now time.Time, actor, requestID string, excursionAudit *model.AuditLog) (int64, error) {
	var invalidated int64
	err := r.store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.ExcursionEvent{}).Where("id = ? AND version = ?", item.ID, expectedVersion).
			Select("*").Omit("id", "code", "created_at", "deleted_at").Updates(item)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrVersionConflict
		}
		decisions := make([]model.DispositionDecision, 0)
		stale := tx.Where("excursion_code = ? AND assessment_version = ? AND invalidated_at IS NULL", item.Code, item.AssessmentVersion)
		if err := stale.Find(&decisions).Error; err != nil {
			return err
		}
		if len(decisions) > 0 {
			updated := tx.Model(&model.DispositionDecision{}).
				Where("excursion_code = ? AND assessment_version = ? AND invalidated_at IS NULL", item.Code, item.AssessmentVersion).
				Updates(map[string]any{
					"invalidated_at":     now,
					"invalidated_reason": reason,
					"version":            gorm.Expr("version + 1"),
					"updated_at":         now,
				})
			if updated.Error != nil {
				return updated.Error
			}
			invalidated = updated.RowsAffected
			for _, decision := range decisions {
				audit := &model.AuditLog{
					Actor: actor, RequestID: requestID, Action: "invalidate", EntityType: "DispositionDecision",
					EntityID: decision.ID, BeforeState: decision.Status, AfterState: decision.Status,
					Detail:    fmt.Sprintf("excursion %s returned for review; assessment v%d decision is history only", item.Code, item.AssessmentVersion),
					CreatedAt: now,
				}
				if err := tx.Create(audit).Error; err != nil {
					return err
				}
			}
		}
		if excursionAudit != nil {
			if err := tx.Create(excursionAudit).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return invalidated, err
}
func (r *excursionEventRepository) Delete(ctx context.Context, id uint) error {
	return r.store.Delete(ctx, id)
}
func (r *excursionEventRepository) CountByStatus(ctx context.Context) (map[string]int64, error) {
	return r.store.CountByStatus(ctx)
}
