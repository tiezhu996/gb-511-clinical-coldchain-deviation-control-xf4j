package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/blueship581/clinical-coldchain-deviation-control/backend/internal/constants"
	"github.com/blueship581/clinical-coldchain-deviation-control/backend/internal/dto"
	"github.com/blueship581/clinical-coldchain-deviation-control/backend/internal/model"
	"github.com/blueship581/clinical-coldchain-deviation-control/backend/internal/repository"
)

type ExcursionEventService interface {
	List(context.Context, dto.PageQuery) (repository.Page[model.ExcursionEvent], error)
	Get(context.Context, uint) (model.ExcursionEvent, error)
	Create(context.Context, dto.CreateExcursionEvent, string, string) (model.ExcursionEvent, error)
	Update(context.Context, uint, dto.UpdateExcursionEvent, string, string) (model.ExcursionEvent, error)
	Transition(context.Context, uint, dto.TransitionRequest, string, string) (model.ExcursionEvent, error)
	Delete(context.Context, uint, string, string) error
	StatusCounts(context.Context) (map[string]int64, error)
	ListAssessments(context.Context, uint) ([]model.ImpactAssessment, error)
	ListDecisions(context.Context, uint) ([]model.DispositionDecision, error)
}

type excursionEventService struct {
	repository  repository.ExcursionEventRepository
	disposition repository.DispositionDecisionRepository
	assessments repository.ImpactAssessmentRepository
	evidence    repository.SensorEvidenceRepository
	security    SecurityService
}

func NewExcursionEventService(repo repository.ExcursionEventRepository, disposition repository.DispositionDecisionRepository, assessments repository.ImpactAssessmentRepository, evidence repository.SensorEvidenceRepository, security SecurityService) ExcursionEventService {
	return &excursionEventService{repository: repo, disposition: disposition, assessments: assessments, evidence: evidence, security: security}
}

func (s *excursionEventService) List(ctx context.Context, query dto.PageQuery) (repository.Page[model.ExcursionEvent], error) {
	return s.repository.List(ctx, query)
}

func (s *excursionEventService) Get(ctx context.Context, id uint) (model.ExcursionEvent, error) {
	return s.repository.Get(ctx, id)
}

func (s *excursionEventService) Create(ctx context.Context, input dto.CreateExcursionEvent, actor, requestID string) (model.ExcursionEvent, error) {
	if err := validateExcursionEventBusinessFields(input.Code, input.Name, input.Facility, input.Owner); err != nil {
		return model.ExcursionEvent{}, err
	}
	item := model.ExcursionEvent{
		BaseModel: model.BaseModel{
			Code: strings.ToUpper(strings.TrimSpace(input.Code)), Name: strings.TrimSpace(input.Name),
			Status: model.ExcursionEventInitialStatus, Version: 1, Description: strings.TrimSpace(input.Description),
		},
		Facility: strings.TrimSpace(input.Facility), Owner: strings.TrimSpace(input.Owner),
		Category: strings.TrimSpace(input.Category), RiskLevel: input.RiskLevel,
		MetricValue: input.MetricValue, MetricUnit: strings.TrimSpace(input.MetricUnit),
		EffectiveAt: input.EffectiveAt.UTC(), Evidence: strings.TrimSpace(input.Evidence),
		RelatedCode:   strings.ToUpper(strings.TrimSpace(input.RelatedCode)),
		ContainerCode: strings.ToUpper(strings.TrimSpace(firstNonEmpty(input.ContainerCode, input.RelatedCode))),
		WindowCode:    strings.ToUpper(strings.TrimSpace(input.WindowCode)),
		ObservedTempC: input.ObservedTempC, DurationMinutes: input.DurationMinutes,
		DetectedAt:     fallbackTime(input.DetectedAt, input.EffectiveAt),
		SensorEvidence: strings.TrimSpace(firstNonEmpty(input.SensorEvidence, input.Evidence)),
		Reviewer:       strings.TrimSpace(input.Reviewer),
	}
	if item.ObservedTempC == 0 {
		item.ObservedTempC = input.MetricValue
	}
	if item.ContainerCode == "" || item.WindowCode == "" || item.SensorEvidence == "" || item.DurationMinutes < 1 {
		return model.ExcursionEvent{}, fmt.Errorf("%w: container, temperature window, duration and sensor evidence are required", ErrInvalidInput)
	}
	if err := s.repository.Create(ctx, &item); err != nil {
		return model.ExcursionEvent{}, fmt.Errorf("create 偏差事件: %w", err)
	}
	_ = s.security.Audit(ctx, actor, requestID, "create", "ExcursionEvent", item.ID, "", item.Status, "created 偏差事件")
	return item, nil
}

func (s *excursionEventService) Update(ctx context.Context, id uint, input dto.UpdateExcursionEvent, actor, requestID string) (model.ExcursionEvent, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.ExcursionEvent{}, err
	}
	if err := validateExcursionEventBusinessFields(current.Code, input.Name, input.Facility, input.Owner); err != nil {
		return model.ExcursionEvent{}, err
	}
	current.Name = strings.TrimSpace(input.Name)
	current.Description = strings.TrimSpace(input.Description)
	current.Facility = strings.TrimSpace(input.Facility)
	current.Owner = strings.TrimSpace(input.Owner)
	current.Category = strings.TrimSpace(input.Category)
	current.RiskLevel = input.RiskLevel
	current.MetricValue = input.MetricValue
	current.MetricUnit = strings.TrimSpace(input.MetricUnit)
	current.EffectiveAt = input.EffectiveAt.UTC()
	current.Evidence = strings.TrimSpace(input.Evidence)
	current.RelatedCode = strings.ToUpper(strings.TrimSpace(input.RelatedCode))
	current.ContainerCode = strings.ToUpper(strings.TrimSpace(firstNonEmpty(input.ContainerCode, input.RelatedCode)))
	current.WindowCode = strings.ToUpper(strings.TrimSpace(input.WindowCode))
	current.ObservedTempC = input.ObservedTempC
	if current.ObservedTempC == 0 {
		current.ObservedTempC = input.MetricValue
	}
	current.DurationMinutes = input.DurationMinutes
	current.DetectedAt = fallbackTime(input.DetectedAt, input.EffectiveAt)
	current.SensorEvidence = strings.TrimSpace(firstNonEmpty(input.SensorEvidence, input.Evidence))
	current.Reviewer = strings.TrimSpace(input.Reviewer)
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()
	if err := s.repository.Update(ctx, id, input.ExpectedVersion, &current); err != nil {
		return model.ExcursionEvent{}, fmt.Errorf("update 偏差事件: %w", err)
	}
	_ = s.security.Audit(ctx, actor, requestID, "update", "ExcursionEvent", id, current.Status, current.Status, "updated business fields")
	return s.repository.Get(ctx, id)
}

func (s *excursionEventService) Transition(ctx context.Context, id uint, input dto.TransitionRequest, actor, requestID string) (model.ExcursionEvent, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.ExcursionEvent{}, err
	}
	target := strings.TrimSpace(input.Status)
	if !constants.CanTransition(constants.ExcursionEventTransitions, current.Status, target) {
		return model.ExcursionEvent{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.Status, target)
	}
	before := current.Status
	evidence := strings.TrimSpace(input.Evidence)
	if evidence == "" {
		evidence = strings.TrimSpace(firstNonEmpty(current.SensorEvidence, current.Evidence))
	}

	switch target {
	case string(constants.ExcursionStateInReview):
		// Decided -> in_review is "退回重审": supersede the current assessment
		// version and invalidate every decision bound to it, atomically.
		if before == string(constants.ExcursionStateDecided) {
			return s.returnForReview(ctx, current, input, evidence, actor, requestID)
		}
		current.Status = target
		current.SensorEvidence = evidence
		current.Evidence = evidence
		current.Reviewer = actor
		current.Version = input.ExpectedVersion + 1
		current.UpdatedAt = time.Now().UTC()
		detail, _ := json.Marshal(map[string]any{"reason": input.Reason, "sensorEvidence": evidence, "containerCode": current.ContainerCode})
		if err := s.repository.Update(ctx, id, input.ExpectedVersion, &current, auditLog(actor, requestID, "transition", "ExcursionEvent", id, before, target, string(detail))); err != nil {
			return model.ExcursionEvent{}, fmt.Errorf("transition 偏差事件: %w", err)
		}
		return s.repository.Get(ctx, id)

	case string(constants.ExcursionStateDecided):
		if evidence == "" {
			return model.ExcursionEvent{}, fmt.Errorf("%w: sensor evidence is required before deciding an excursion", ErrInvalidInput)
		}
		if count, err := s.evidence.CountForExcursion(ctx, current.Code); err != nil || count == 0 {
			return model.ExcursionEvent{}, fmt.Errorf("%w: registered sensor evidence is required before deciding an excursion", ErrInvalidInput)
		}
		return s.completeAssessment(ctx, current, input, evidence, actor, requestID)

	case string(constants.ExcursionStateClosed):
		if current.CurrentAssessmentVersion == nil {
			return model.ExcursionEvent{}, fmt.Errorf("%w: the deviation has no current impact assessment version", ErrInvalidInput)
		}
		final, err := s.disposition.HasEffectiveFinalForExcursion(ctx, current.Code, *current.CurrentAssessmentVersion)
		if err != nil {
			return model.ExcursionEvent{}, err
		}
		if !final {
			return model.ExcursionEvent{}, fmt.Errorf("%w: closing requires an independently approved final decision consistent with current assessment version %d", ErrInvalidInput, *current.CurrentAssessmentVersion)
		}
		current.Status = target
		current.SensorEvidence = evidence
		current.Evidence = evidence
		current.Version = input.ExpectedVersion + 1
		current.UpdatedAt = time.Now().UTC()
		detail, _ := json.Marshal(map[string]any{
			"reason": input.Reason, "sensorEvidence": evidence, "containerCode": current.ContainerCode,
			"assessmentVersion": *current.CurrentAssessmentVersion,
		})
		if err := s.repository.Update(ctx, id, input.ExpectedVersion, &current, auditLog(actor, requestID, "transition", "ExcursionEvent", id, before, target, string(detail))); err != nil {
			return model.ExcursionEvent{}, fmt.Errorf("transition 偏差事件: %w", err)
		}
		return s.repository.Get(ctx, id)

	default:
		return model.ExcursionEvent{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, before, target)
	}
}

// completeAssessment generates a new ImpactAssessment version and points the
// deviation at it inside one transaction.
func (s *excursionEventService) completeAssessment(ctx context.Context, current model.ExcursionEvent, input dto.TransitionRequest, evidence, actor, requestID string) (model.ExcursionEvent, error) {
	// Version follows the highest historical version, so a re-evaluation after
	// return-for-review always produces a brand-new version number.
	nextVersion := uint(1)
	if latest, err := s.assessments.LatestForExcursion(ctx, current.Code); err == nil {
		nextVersion = latest.AssessmentVersion + 1
	}
	now := time.Now().UTC()
	assessment := &model.ImpactAssessment{
		ExcursionCode:       current.Code,
		AssessmentVersion:   nextVersion,
		Summary:             strings.TrimSpace(input.Reason),
		ImpactLevel:         current.RiskLevel,
		AffectedProduct:     strings.TrimSpace(current.Category),
		StabilityConclusion: strings.TrimSpace(firstNonEmpty(current.Description, input.Reason)),
		RiskLevel:           current.RiskLevel,
		ObservedTempC:       current.ObservedTempC,
		DurationMinutes:     current.DurationMinutes,
		SensorEvidence:      evidence,
		EvaluatedBy:         actor,
		EvaluatedAt:         now,
		Status:              string(constants.AssessmentStateCurrent),
	}
	detail, _ := json.Marshal(map[string]any{
		"reason": input.Reason, "sensorEvidence": evidence, "containerCode": current.ContainerCode,
		"assessmentVersion": nextVersion,
	})
	updated, _, err := s.assessments.CompleteAssessment(ctx, repository.CompleteAssessmentInput{
		ExcursionID:              current.ID,
		Assessment:               assessment,
		ExpectedAggregateVersion: input.ExpectedVersion,
		Audit:                    auditLog(actor, requestID, "transition", "ExcursionEvent", current.ID, current.Status, "decided", string(detail)),
	})
	if err != nil {
		return model.ExcursionEvent{}, fmt.Errorf("complete impact assessment: %w", err)
	}
	return updated, nil
}

// returnForReview supersedes the current assessment version and invalidates the
// decisions referencing it. Superseded decisions remain as history but can no
// longer be approved or close the deviation.
func (s *excursionEventService) returnForReview(ctx context.Context, current model.ExcursionEvent, input dto.TransitionRequest, evidence, actor, requestID string) (model.ExcursionEvent, error) {
	reason := strings.TrimSpace(input.Reason)
	updated, _, err := s.assessments.ReturnForReview(ctx, repository.ReturnForReviewInput{
		ExcursionID:              current.ID,
		ExpectedAggregateVersion: input.ExpectedVersion,
		Reason:                   reason,
		Actor:                    actor,
		RequestID:                requestID,
		DecisionAuditAction:      "assessment_invalidated",
		Audit: auditLog(actor, requestID, "transition", "ExcursionEvent", current.ID, "decided", "in_review",
			mustJSON(map[string]any{
				"reason": reason, "sensorEvidence": evidence, "containerCode": current.ContainerCode,
				"supersededVersion": *current.CurrentAssessmentVersion,
			})),
	})
	if err != nil {
		return model.ExcursionEvent{}, fmt.Errorf("return excursion for re-review: %w", err)
	}
	return updated, nil
}

func (s *excursionEventService) Delete(ctx context.Context, id uint, actor, requestID string) error {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repository.Delete(ctx, id); err != nil {
		return err
	}
	return s.security.Audit(ctx, actor, requestID, "delete", "ExcursionEvent", id, current.Status, "deleted", "soft deleted 偏差事件")
}

func (s *excursionEventService) StatusCounts(ctx context.Context) (map[string]int64, error) {
	return s.repository.CountByStatus(ctx)
}

func (s *excursionEventService) ListAssessments(ctx context.Context, id uint) ([]model.ImpactAssessment, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.assessments.ListForExcursion(ctx, current.Code)
}

func (s *excursionEventService) ListDecisions(ctx context.Context, id uint) ([]model.DispositionDecision, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.disposition.ListForExcursion(ctx, current.Code)
}

func validateExcursionEventBusinessFields(code, name, facility, owner string) error {
	if strings.TrimSpace(code) == "" || strings.TrimSpace(name) == "" || strings.TrimSpace(facility) == "" || strings.TrimSpace(owner) == "" {
		return ErrInvalidInput
	}
	return nil
}

func mustJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}
