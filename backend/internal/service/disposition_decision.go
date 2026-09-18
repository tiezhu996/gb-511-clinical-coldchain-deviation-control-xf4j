package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blueship581/clinical-coldchain-deviation-control/backend/internal/constants"
	"github.com/blueship581/clinical-coldchain-deviation-control/backend/internal/dto"
	"github.com/blueship581/clinical-coldchain-deviation-control/backend/internal/model"
	"github.com/blueship581/clinical-coldchain-deviation-control/backend/internal/repository"
)

type DispositionDecisionService interface {
	List(context.Context, dto.PageQuery) (repository.Page[model.DispositionDecision], error)
	Get(context.Context, uint) (model.DispositionDecision, error)
	Create(context.Context, dto.CreateDispositionDecision, string, string) (model.DispositionDecision, error)
	Update(context.Context, uint, dto.UpdateDispositionDecision, string, string) (model.DispositionDecision, error)
	Transition(context.Context, uint, dto.TransitionRequest, string, string) (model.DispositionDecision, error)
	Delete(context.Context, uint, string, string) error
	StatusCounts(context.Context) (map[string]int64, error)
}

type dispositionDecisionService struct {
	repository  repository.DispositionDecisionRepository
	assessments repository.ImpactAssessmentRepository
	excursions  repository.ExcursionEventRepository
	evidence    repository.SensorEvidenceRepository
	security    SecurityService
}

func NewDispositionDecisionService(repo repository.DispositionDecisionRepository, assessments repository.ImpactAssessmentRepository, excursions repository.ExcursionEventRepository, evidence repository.SensorEvidenceRepository, security SecurityService) DispositionDecisionService {
	return &dispositionDecisionService{repository: repo, assessments: assessments, excursions: excursions, evidence: evidence, security: security}
}

func (s *dispositionDecisionService) List(ctx context.Context, query dto.PageQuery) (repository.Page[model.DispositionDecision], error) {
	return s.repository.List(ctx, query)
}

func (s *dispositionDecisionService) Get(ctx context.Context, id uint) (model.DispositionDecision, error) {
	return s.repository.Get(ctx, id)
}

func (s *dispositionDecisionService) Create(ctx context.Context, input dto.CreateDispositionDecision, actor, requestID string) (model.DispositionDecision, error) {
	if err := validateDispositionDecisionBusinessFields(input.Code, input.Name, input.Facility, input.Owner); err != nil {
		return model.DispositionDecision{}, err
	}
	item := model.DispositionDecision{
		BaseModel: model.BaseModel{
			Code: strings.ToUpper(strings.TrimSpace(input.Code)), Name: strings.TrimSpace(input.Name),
			Status: model.DispositionDecisionInitialStatus, Version: 1, Description: strings.TrimSpace(input.Description),
		},
		Facility: strings.TrimSpace(input.Facility), Owner: strings.TrimSpace(input.Owner),
		Category: strings.TrimSpace(input.Category), RiskLevel: input.RiskLevel,
		MetricValue: input.MetricValue, MetricUnit: strings.TrimSpace(input.MetricUnit),
		EffectiveAt: input.EffectiveAt.UTC(), Evidence: strings.TrimSpace(input.Evidence),
		RelatedCode:    strings.ToUpper(strings.TrimSpace(input.RelatedCode)),
		ExcursionCode:  strings.ToUpper(strings.TrimSpace(firstNonEmpty(input.ExcursionCode, input.RelatedCode))),
		DecisionBasis:  strings.TrimSpace(firstNonEmpty(input.DecisionBasis, input.Description)),
		SensorEvidence: strings.TrimSpace(firstNonEmpty(input.SensorEvidence, input.Evidence)),
		ProposedBy:     actor,
	}
	if item.ExcursionCode == "" || item.SensorEvidence == "" {
		return model.DispositionDecision{}, fmt.Errorf("%w: excursion code and sensor evidence are required", ErrInvalidInput)
	}
	if count, err := s.evidence.CountForExcursion(ctx, item.ExcursionCode); err != nil || count == 0 {
		return model.DispositionDecision{}, fmt.Errorf("%w: registered sensor evidence is required for the excursion", ErrInvalidInput)
	}
	// The proposal is pinned to the assessment version currently effective on
	// the deviation, re-checked under a row lock against concurrent return.
	audit := auditLog(actor, requestID, "create", "DispositionDecision", 0, "", "draft",
		fmt.Sprintf("created 处置决定 for excursion=%s", item.ExcursionCode))
	created, _, err := s.assessments.CreateProposal(ctx, repository.CreateProposalInput{Decision: &item, Audit: audit})
	if err != nil {
		if errors.Is(err, repository.ErrStaleAssessment) {
			return model.DispositionDecision{}, fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
		}
		return model.DispositionDecision{}, fmt.Errorf("create 处置决定: %w", err)
	}
	return created, nil
}

func (s *dispositionDecisionService) Update(ctx context.Context, id uint, input dto.UpdateDispositionDecision, actor, requestID string) (model.DispositionDecision, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.DispositionDecision{}, err
	}
	if current.Status != "draft" || !strings.EqualFold(current.ProposedBy, actor) {
		return model.DispositionDecision{}, fmt.Errorf("%w: only the proposer may edit a draft disposition", ErrInvalidInput)
	}
	if strings.TrimSpace(current.InvalidatedReason) != "" || current.AssessmentVersion == nil {
		return model.DispositionDecision{}, fmt.Errorf("%w: proposal is historical after assessment version %d and cannot be edited", ErrInvalidInput, assessmentVersionOf(current))
	}
	excursion, err := s.excursions.GetByCode(ctx, current.ExcursionCode)
	if err != nil {
		return model.DispositionDecision{}, err
	}
	if excursion.CurrentAssessmentVersion == nil || *excursion.CurrentAssessmentVersion != *current.AssessmentVersion {
		return model.DispositionDecision{}, fmt.Errorf("%w: assessment version %d is no longer current; raise a new proposal", ErrStaleAssessment, assessmentVersionOf(current))
	}
	if err := validateDispositionDecisionBusinessFields(current.Code, input.Name, input.Facility, input.Owner); err != nil {
		return model.DispositionDecision{}, err
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
	current.ExcursionCode = strings.ToUpper(strings.TrimSpace(firstNonEmpty(input.ExcursionCode, input.RelatedCode)))
	current.DecisionBasis = strings.TrimSpace(firstNonEmpty(input.DecisionBasis, input.Description))
	current.SensorEvidence = strings.TrimSpace(firstNonEmpty(input.SensorEvidence, input.Evidence))
	if current.ExcursionCode == "" || current.DecisionBasis == "" || current.SensorEvidence == "" {
		return model.DispositionDecision{}, fmt.Errorf("%w: excursion, basis and sensor evidence cannot be cleared", ErrInvalidInput)
	}
	if count, err := s.evidence.CountForExcursion(ctx, current.ExcursionCode); err != nil || count == 0 {
		return model.DispositionDecision{}, fmt.Errorf("%w: registered sensor evidence is required for the excursion", ErrInvalidInput)
	}
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()
	if err := s.repository.Update(ctx, id, input.ExpectedVersion, &current); err != nil {
		return model.DispositionDecision{}, fmt.Errorf("update 处置决定: %w", err)
	}
	_ = s.security.Audit(ctx, actor, requestID, "update", "DispositionDecision", id, current.Status, current.Status,
		fmt.Sprintf("updated draft proposal on assessment version %d", *current.AssessmentVersion))
	return s.repository.Get(ctx, id)
}

func (s *dispositionDecisionService) Transition(ctx context.Context, id uint, input dto.TransitionRequest, actor, requestID string) (model.DispositionDecision, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.DispositionDecision{}, err
	}
	target := strings.TrimSpace(input.Status)
	if !constants.CanTransition(constants.DispositionDecisionTransitions, current.Status, target) {
		return model.DispositionDecision{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.Status, target)
	}
	if err := validateIndependentApproval(current.ProposedBy, actor); err != nil {
		return model.DispositionDecision{}, err
	}
	evidence := strings.TrimSpace(input.Evidence)
	if evidence == "" {
		evidence = strings.TrimSpace(firstNonEmpty(current.SensorEvidence, current.Evidence))
	}
	if evidence == "" {
		return model.DispositionDecision{}, fmt.Errorf("%w: sensor evidence is required for a disposition decision", ErrInvalidInput)
	}
	if count, err := s.evidence.CountForExcursion(ctx, current.ExcursionCode); err != nil || count == 0 {
		return model.DispositionDecision{}, fmt.Errorf("%w: registered sensor evidence is required for approval", ErrInvalidInput)
	}
	// Approval and a concurrent return-for-review are reconciled under a lock on
	// the deviation: only one can end up effective for the current version.
	detail, _ := json.Marshal(map[string]any{
		"reason": input.Reason, "sensorEvidence": evidence, "excursionCode": current.ExcursionCode,
		"proposedBy": current.ProposedBy, "approvedBy": actor,
		"assessmentVersion": assessmentVersionOf(current),
	})
	approved, _, err := s.assessments.ApproveFinalDecision(ctx, repository.ApproveFinalDecisionInput{
		DecisionID:              id,
		ExpectedDecisionVersion: input.ExpectedVersion,
		TargetStatus:            target,
		Evidence:                evidence,
		Reason:                  strings.TrimSpace(input.Reason),
		Actor:                   actor,
		Audit:                   auditLog(actor, requestID, "transition", "DispositionDecision", id, "draft", target, string(detail)),
	})
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrStaleAssessment):
			return model.DispositionDecision{}, fmt.Errorf("%w: decision references assessment version %d which is no longer current; approve a newly proposed decision instead", ErrStaleAssessment, assessmentVersionOf(current))
		case errors.Is(err, repository.ErrInvalidWorkflowState):
			return model.DispositionDecision{}, fmt.Errorf("%w: %s", ErrInvalidTransition, err.Error())
		default:
			return model.DispositionDecision{}, fmt.Errorf("transition 处置决定: %w", err)
		}
	}
	return approved, nil
}

func (s *dispositionDecisionService) Delete(ctx context.Context, id uint, actor, requestID string) error {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repository.Delete(ctx, id); err != nil {
		return err
	}
	return s.security.Audit(ctx, actor, requestID, "delete", "DispositionDecision", id, current.Status, "deleted", "soft deleted 处置决定")
}

func (s *dispositionDecisionService) StatusCounts(ctx context.Context) (map[string]int64, error) {
	return s.repository.CountByStatus(ctx)
}

func validateDispositionDecisionBusinessFields(code, name, facility, owner string) error {
	if strings.TrimSpace(code) == "" || strings.TrimSpace(name) == "" || strings.TrimSpace(facility) == "" || strings.TrimSpace(owner) == "" {
		return ErrInvalidInput
	}
	return nil
}

func validateIndependentApproval(proposedBy, approvedBy string) error {
	proposer := strings.TrimSpace(proposedBy)
	approver := strings.TrimSpace(approvedBy)
	if proposer == "" || approver == "" {
		return fmt.Errorf("%w: proposer and approver identities are required", ErrInvalidInput)
	}
	if strings.EqualFold(proposer, approver) {
		return fmt.Errorf("%w: disposition requires an independent second reviewer", ErrInvalidInput)
	}
	return nil
}

func assessmentVersionOf(decision model.DispositionDecision) uint {
	if decision.AssessmentVersion == nil {
		return 0
	}
	return *decision.AssessmentVersion
}
