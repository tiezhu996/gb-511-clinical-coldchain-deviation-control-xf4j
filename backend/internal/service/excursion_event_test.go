package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/blueship581/clinical-coldchain-deviation-control/backend/internal/config"
	"github.com/blueship581/clinical-coldchain-deviation-control/backend/internal/constants"
	"github.com/blueship581/clinical-coldchain-deviation-control/backend/internal/dto"
	"github.com/blueship581/clinical-coldchain-deviation-control/backend/internal/model"
	"github.com/blueship581/clinical-coldchain-deviation-control/backend/internal/repository"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type assessmentFixture struct {
	db           *gorm.DB
	excursions   ExcursionEventService
	dispositions DispositionDecisionService
}

func newAssessmentFixture(t *testing.T) assessmentFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("unwrap db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(
		&model.Role{}, &model.User{}, &model.AuditLog{}, &model.SensorEvidence{},
		&model.TransportContainer{}, &model.TemperatureWindow{},
		&model.ExcursionEvent{}, &model.DispositionDecision{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	security := NewSecurityService(repository.NewSecurityRepository(db), config.Config{})
	excursionRepo := repository.NewExcursionEventRepository(db)
	dispositionRepo := repository.NewDispositionDecisionRepository(db)
	evidenceRepo := repository.NewSensorEvidenceRepository(db)
	return assessmentFixture{
		db:           db,
		excursions:   NewExcursionEventService(excursionRepo, dispositionRepo, evidenceRepo, security),
		dispositions: NewDispositionDecisionService(dispositionRepo, evidenceRepo, security),
	}
}

func (f assessmentFixture) addExcursion(t *testing.T, code, status string, assessmentVersion uint) model.ExcursionEvent {
	t.Helper()
	item := model.ExcursionEvent{
		BaseModel:     model.BaseModel{Code: code, Name: code + " 偏差", Status: status, Version: 1},
		ContainerCode: "TC-001", WindowCode: "TW-001", ObservedTempC: 8.4, DurationMinutes: 5,
		DetectedAt: time.Now().UTC(), SensorEvidence: "minio://sensor/tc-001/trace.csv",
		Facility: "上海配送中心", Owner: "reviewer", Category: "短时高温", RiskLevel: "low",
		EffectiveAt: time.Now().UTC(), Evidence: "minio://sensor/tc-001/trace.csv",
		AssessmentVersion: assessmentVersion,
	}
	if err := f.db.Create(&item).Error; err != nil {
		t.Fatalf("seed excursion: %v", err)
	}
	return item
}

func (f assessmentFixture) addEvidence(t *testing.T, excursionCode string) {
	t.Helper()
	item := model.SensorEvidence{Code: "SE-" + excursionCode, ExcursionCode: excursionCode, ContainerCode: "TC-001",
		ObjectKey: "sensor/tc-001/" + excursionCode + ".csv", SHA256: "abc", CapturedAt: time.Now().UTC(), CapturedBy: "tester"}
	if err := f.db.Create(&item).Error; err != nil {
		t.Fatalf("seed evidence: %v", err)
	}
}

func (f assessmentFixture) addDecision(t *testing.T, code, excursionCode, status string, assessmentVersion uint) model.DispositionDecision {
	t.Helper()
	item := model.DispositionDecision{
		BaseModel:     model.BaseModel{Code: code, Name: code + " 决定", Status: status, Version: 1},
		ExcursionCode: excursionCode, DecisionBasis: "评估依据", SensorEvidence: "minio://sensor/tc-001/trace.csv",
		ProposedBy: "operator", Facility: "质量放行组", Owner: "operator", Category: "放行提议",
		RiskLevel: "low", EffectiveAt: time.Now().UTC(), Evidence: "minio://sensor/tc-001/trace.csv",
		AssessmentVersion: assessmentVersion,
	}
	if status != "draft" {
		item.ApprovedBy = "reviewer"
		now := time.Now().UTC()
		item.DecidedAt = &now
	}
	if err := f.db.Create(&item).Error; err != nil {
		t.Fatalf("seed decision: %v", err)
	}
	return item
}

func (f assessmentFixture) getExcursion(t *testing.T, id uint) model.ExcursionEvent {
	t.Helper()
	item, err := f.excursions.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get excursion: %v", err)
	}
	return item
}

func (f assessmentFixture) getDecision(t *testing.T, id uint) model.DispositionDecision {
	t.Helper()
	item, err := f.dispositions.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get decision: %v", err)
	}
	return item
}

func transitionExcursion(t *testing.T, f assessmentFixture, item model.ExcursionEvent, target string) (model.ExcursionEvent, error) {
	t.Helper()
	return f.excursions.Transition(context.Background(), item.ID, dto.TransitionRequest{
		Status: target, ExpectedVersion: item.Version, Reason: "质量复核员处理", Evidence: item.SensorEvidence,
	}, "reviewer", "req-test")
}

func TestAssessmentVersionGeneratedOnDecide(t *testing.T) {
	f := newAssessmentFixture(t)
	excursion := f.addExcursion(t, "EE-T1", "open", 0)
	f.addEvidence(t, "EE-T1")

	excursion, err := transitionExcursion(t, f, excursion, "in_review")
	if err != nil {
		t.Fatalf("open -> in_review: %v", err)
	}
	if excursion.AssessmentVersion != 0 {
		t.Fatalf("review must not generate a version, got %d", excursion.AssessmentVersion)
	}
	excursion, err = transitionExcursion(t, f, excursion, "decided")
	if err != nil {
		t.Fatalf("in_review -> decided: %v", err)
	}
	if excursion.AssessmentVersion != 1 {
		t.Fatalf("first assessment must be v1, got %d", excursion.AssessmentVersion)
	}
	excursion, err = transitionExcursion(t, f, excursion, "in_review")
	if err != nil {
		t.Fatalf("decided -> in_review (return): %v", err)
	}
	if excursion.AssessmentVersion != 1 {
		t.Fatalf("return must keep the current version, got %d", excursion.AssessmentVersion)
	}
	excursion, err = transitionExcursion(t, f, excursion, "decided")
	if err != nil {
		t.Fatalf("re-assessment: %v", err)
	}
	if excursion.AssessmentVersion != 2 {
		t.Fatalf("re-assessment must generate v2, got %d", excursion.AssessmentVersion)
	}
}

func TestReturnForReviewInvalidatesCurrentVersionDecisions(t *testing.T) {
	f := newAssessmentFixture(t)
	excursion := f.addExcursion(t, "EE-T2", "decided", 1)
	f.addEvidence(t, "EE-T2")
	draft := f.addDecision(t, "DD-T2A", "EE-T2", "draft", 1)
	final := f.addDecision(t, "DD-T2B", "EE-T2", "release", 1)
	older := f.addDecision(t, "DD-T2C", "EE-T2", "discard", 0)

	if _, err := transitionExcursion(t, f, excursion, "in_review"); err != nil {
		t.Fatalf("return for review: %v", err)
	}
	for _, id := range []uint{draft.ID, final.ID} {
		decision := f.getDecision(t, id)
		if decision.InvalidatedAt == nil || decision.InvalidatedReason != constants.DecisionInvalidReasonExcursionReturned {
			t.Fatalf("decision %d must be invalidated as returned-for-review, got %+v", id, decision)
		}
	}
	if kept := f.getDecision(t, older.ID); kept.InvalidatedAt != nil {
		t.Fatalf("decision of an older assessment version must stay untouched, got %+v", kept)
	}
	var invalidations int64
	if err := f.db.Model(&model.AuditLog{}).Where("action = ? AND entity_type = ?", "invalidate", "DispositionDecision").Count(&invalidations).Error; err != nil {
		t.Fatalf("count invalidation audits: %v", err)
	}
	if invalidations != 2 {
		t.Fatalf("expected 2 invalidation audit entries, got %d", invalidations)
	}
}

func TestCloseRequiresCurrentVersionFinalApprovedBySecondPerson(t *testing.T) {
	f := newAssessmentFixture(t)
	excursion := f.addExcursion(t, "EE-T3", "decided", 1)
	f.addEvidence(t, "EE-T3")

	if _, err := transitionExcursion(t, f, excursion, "closed"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("close without a final decision must fail, got %v", err)
	}
	draft := f.addDecision(t, "DD-T3", "EE-T3", "draft", 1)
	if _, err := transitionExcursion(t, f, excursion, "closed"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("close with only a draft must fail, got %v", err)
	}
	if _, err := f.dispositions.Transition(context.Background(), draft.ID, dto.TransitionRequest{
		Status: "release", ExpectedVersion: draft.Version, Reason: "提议人自我批准", Evidence: draft.SensorEvidence,
	}, "operator", "req-test"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("self approval must stay rejected, got %v", err)
	}
	if _, err := f.dispositions.Transition(context.Background(), draft.ID, dto.TransitionRequest{
		Status: "release", ExpectedVersion: draft.Version, Reason: "独立复核批准放行", Evidence: draft.SensorEvidence,
	}, "reviewer", "req-test"); err != nil {
		t.Fatalf("independent approval: %v", err)
	}
	excursion = f.getExcursion(t, excursion.ID)
	if _, err := transitionExcursion(t, f, excursion, "closed"); err != nil {
		t.Fatalf("close with a valid current-version final decision: %v", err)
	}
}

func TestOldDecisionCannotCloseAfterReassessment(t *testing.T) {
	f := newAssessmentFixture(t)
	excursion := f.addExcursion(t, "EE-T4", "decided", 1)
	f.addEvidence(t, "EE-T4")
	f.addDecision(t, "DD-T4", "EE-T4", "release", 1)

	excursion, err := transitionExcursion(t, f, excursion, "in_review")
	if err != nil {
		t.Fatalf("return for review: %v", err)
	}
	excursion, err = transitionExcursion(t, f, excursion, "decided")
	if err != nil {
		t.Fatalf("re-assessment: %v", err)
	}
	if excursion.AssessmentVersion != 2 {
		t.Fatalf("expected assessment v2, got %d", excursion.AssessmentVersion)
	}
	if _, err := transitionExcursion(t, f, excursion, "closed"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalidated v1 decision must not close the reassessed excursion, got %v", err)
	}
	proposal, err := f.dispositions.Create(context.Background(), dto.CreateDispositionDecision{
		Code: "DD-T4B", Name: "v2 放行提议", Facility: "质量放行组", Owner: "operator", Category: "放行提议",
		RiskLevel: "low", EffectiveAt: time.Now().UTC(), Evidence: "minio://sensor/tc-001/trace.csv",
		ExcursionCode: "EE-T4", DecisionBasis: "重新评估后的放行依据", SensorEvidence: "minio://sensor/tc-001/trace.csv",
	}, "operator", "req-test")
	if err != nil {
		t.Fatalf("create v2 proposal: %v", err)
	}
	if proposal.AssessmentVersion != 2 {
		t.Fatalf("new proposal must reference current version v2, got %d", proposal.AssessmentVersion)
	}
	if _, err := f.dispositions.Transition(context.Background(), proposal.ID, dto.TransitionRequest{
		Status: "release", ExpectedVersion: proposal.Version, Reason: "独立复核批准 v2 放行", Evidence: proposal.SensorEvidence,
	}, "reviewer", "req-test"); err != nil {
		t.Fatalf("approve v2 proposal: %v", err)
	}
	excursion = f.getExcursion(t, excursion.ID)
	if _, err := transitionExcursion(t, f, excursion, "closed"); err != nil {
		t.Fatalf("close with a v2 independently approved decision: %v", err)
	}
}

func TestProposalReferencesCurrentVersionOnly(t *testing.T) {
	f := newAssessmentFixture(t)
	f.addExcursion(t, "EE-T5", "in_review", 0)
	f.addEvidence(t, "EE-T5")
	input := dto.CreateDispositionDecision{
		Code: "DD-T5", Name: "未评估偏差提议", Facility: "质量放行组", Owner: "operator", Category: "放行提议",
		RiskLevel: "low", EffectiveAt: time.Now().UTC(), Evidence: "minio://sensor/tc-001/trace.csv",
		ExcursionCode: "EE-T5", DecisionBasis: "提前提议", SensorEvidence: "minio://sensor/tc-001/trace.csv",
	}
	if _, err := f.dispositions.Create(context.Background(), input, "operator", "req-test"); !errors.Is(err, repository.ErrAssessmentStale) {
		t.Fatalf("proposal for a non-decided excursion must be rejected, got %v", err)
	}
}

func TestConcurrentReturnAndApprovalKeepOneValidResult(t *testing.T) {
	f := newAssessmentFixture(t)
	excursion := f.addExcursion(t, "EE-T6", "decided", 1)
	f.addEvidence(t, "EE-T6")
	draft := f.addDecision(t, "DD-T6", "EE-T6", "draft", 1)

	// Approval commits first, then the return wins the excursion and invalidates it.
	if _, err := f.dispositions.Transition(context.Background(), draft.ID, dto.TransitionRequest{
		Status: "release", ExpectedVersion: draft.Version, Reason: "独立复核批准放行", Evidence: draft.SensorEvidence,
	}, "reviewer", "req-test"); err != nil {
		t.Fatalf("approval before return: %v", err)
	}
	excursion, err := transitionExcursion(t, f, excursion, "in_review")
	if err != nil {
		t.Fatalf("return after approval: %v", err)
	}
	decision := f.getDecision(t, draft.ID)
	if decision.InvalidatedAt == nil {
		t.Fatal("return must invalidate the concurrently approved decision")
	}
	excursion, err = transitionExcursion(t, f, excursion, "decided")
	if err != nil {
		t.Fatalf("re-assessment: %v", err)
	}
	if _, err := transitionExcursion(t, f, excursion, "closed"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("no valid result may remain for closing, got %v", err)
	}

	// Return commits first, then the stale approval must fail.
	excursion2 := f.addExcursion(t, "EE-T7", "decided", 1)
	f.addEvidence(t, "EE-T7")
	draft2 := f.addDecision(t, "DD-T7", "EE-T7", "draft", 1)
	if _, err := transitionExcursion(t, f, excursion2, "in_review"); err != nil {
		t.Fatalf("return before approval: %v", err)
	}
	if _, err := f.dispositions.Transition(context.Background(), draft2.ID, dto.TransitionRequest{
		Status: "release", ExpectedVersion: draft2.Version, Reason: "迟到批准", Evidence: draft2.SensorEvidence,
	}, "reviewer", "req-test"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("approval after return must fail, got %v", err)
	}
}

func TestInvalidatedDecisionIsReadOnlyHistory(t *testing.T) {
	f := newAssessmentFixture(t)
	excursion := f.addExcursion(t, "EE-T8", "decided", 1)
	f.addEvidence(t, "EE-T8")
	draft := f.addDecision(t, "DD-T8", "EE-T8", "draft", 1)
	if _, err := transitionExcursion(t, f, excursion, "in_review"); err != nil {
		t.Fatalf("return for review: %v", err)
	}
	decision := f.getDecision(t, draft.ID)
	if _, err := f.dispositions.Update(context.Background(), decision.ID, dto.UpdateDispositionDecision{
		ExpectedVersion: decision.Version, Name: "改写历史", Facility: "质量放行组", Owner: "operator",
		Category: "放行提议", RiskLevel: "low", EffectiveAt: time.Now().UTC(),
		ExcursionCode: "EE-T8", DecisionBasis: "篡改", SensorEvidence: "minio://sensor/tc-001/trace.csv",
	}, "operator", "req-test"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("editing an invalidated decision must fail, got %v", err)
	}
	if _, err := f.dispositions.Transition(context.Background(), decision.ID, dto.TransitionRequest{
		Status: "release", ExpectedVersion: decision.Version, Reason: "迟到批准", Evidence: decision.SensorEvidence,
	}, "reviewer", "req-test"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("approving an invalidated decision must fail, got %v", err)
	}
}

func TestConcurrentTransitionsKeepSingleWinner(t *testing.T) {
	f := newAssessmentFixture(t)
	excursion := f.addExcursion(t, "EE-T9", "decided", 1)
	f.addEvidence(t, "EE-T9")
	f.addDecision(t, "DD-T9", "EE-T9", "release", 1)

	// Service level: a stale request is rejected by the state graph after re-reading.
	if _, err := transitionExcursion(t, f, excursion, "closed"); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if _, err := transitionExcursion(t, f, excursion, "in_review"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("stale return must be rejected, got %v", err)
	}

	// Repository level: two commits racing with the same expected version keep one winner.
	repo := repository.NewExcursionEventRepository(f.db)
	race := f.addExcursion(t, "EE-T10", "decided", 1)
	closing := race
	closing.Status, closing.Version = "closed", race.Version+1
	if err := repo.Update(context.Background(), race.ID, race.Version, &closing); err != nil {
		t.Fatalf("first commit: %v", err)
	}
	returning := race
	returning.Status, returning.Version = "in_review", race.Version+1
	if err := repo.Update(context.Background(), race.ID, race.Version, &returning); !errors.Is(err, repository.ErrVersionConflict) {
		t.Fatalf("second commit with the same version must lose, got %v", err)
	}
}
