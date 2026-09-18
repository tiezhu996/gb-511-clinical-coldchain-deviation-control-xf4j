package repository

import (
	"context"

	"github.com/blueship581/clinical-coldchain-deviation-control/backend/internal/dto"
	"github.com/blueship581/clinical-coldchain-deviation-control/backend/internal/model"
	"gorm.io/gorm"
)

// DispositionDecisionRepository owns all persistence operations for 处置决定.
type DispositionDecisionRepository interface {
	List(context.Context, dto.PageQuery) (Page[model.DispositionDecision], error)
	Get(context.Context, uint) (model.DispositionDecision, error)
	Create(context.Context, *model.DispositionDecision) error
	Update(context.Context, uint, uint, *model.DispositionDecision, ...*model.AuditLog) error
	Delete(context.Context, uint) error
	CountByStatus(context.Context) (map[string]int64, error)
	ListForExcursion(ctx context.Context, excursionCode string) ([]model.DispositionDecision, error)
	// HasEffectiveFinalForExcursion reports whether a final, never-invalidated
	// decision exists for the assessment version currently effective on the
	// deviation.
	HasEffectiveFinalForExcursion(ctx context.Context, excursionCode string, assessmentVersion uint) (bool, error)
}

type dispositionDecisionRepository struct {
	store *Store[model.DispositionDecision]
	db    *gorm.DB
}

func NewDispositionDecisionRepository(db *gorm.DB) DispositionDecisionRepository {
	return &dispositionDecisionRepository{store: NewStore[model.DispositionDecision](db), db: db}
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
func (r *dispositionDecisionRepository) Update(ctx context.Context, id, version uint, item *model.DispositionDecision, audits ...*model.AuditLog) error {
	return r.store.Update(ctx, id, version, item, audits...)
}
func (r *dispositionDecisionRepository) Delete(ctx context.Context, id uint) error {
	return r.store.Delete(ctx, id)
}
func (r *dispositionDecisionRepository) CountByStatus(ctx context.Context) (map[string]int64, error) {
	return r.store.CountByStatus(ctx)
}
func (r *dispositionDecisionRepository) ListForExcursion(ctx context.Context, code string) ([]model.DispositionDecision, error) {
	items := make([]model.DispositionDecision, 0)
	err := r.db.WithContext(ctx).Where("excursion_code = ?", code).
		Order("updated_at DESC, id DESC").Find(&items).Error
	return items, err
}
func (r *dispositionDecisionRepository) HasEffectiveFinalForExcursion(ctx context.Context, code string, assessmentVersion uint) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.DispositionDecision{}).
		Where("excursion_code = ? AND assessment_version = ? AND invalidated_reason = '' AND status IN ?",
			code, assessmentVersion, []string{"release", "quarantine", "discard"}).
		Count(&count).Error
	return count > 0, err
}
