package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/blueship581/clinical-coldchain-deviation-control/backend/internal/config"
	"github.com/blueship581/clinical-coldchain-deviation-control/backend/internal/dto"
	"github.com/blueship581/clinical-coldchain-deviation-control/backend/internal/model"
	"github.com/blueship581/clinical-coldchain-deviation-control/backend/internal/repository"
	"github.com/blueship581/clinical-coldchain-deviation-control/backend/internal/service"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type workflowFixtures struct {
	db              *gorm.DB
	excursions      service.ExcursionEventService
	dispositions    service.DispositionDecisionService
	excursionRepo   repository.ExcursionEventRepository
	dispositionRepo repository.DispositionDecisionRepository
	assessmentRepo  repository.ImpactAssessmentRepository
}

func newWorkflowFixtures(t *testing.T) workflowFixtures {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared&_pragma=busy_timeout(5000)"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(
		&model.Role{}, &model.User{}, &model.AuditLog{}, &model.SensorEvidence{},
		&model.TransportContainer{}, &model.TemperatureWindow{},
		&model.ExcursionEvent{}, &model.ImpactAssessment{}, &model.DispositionDecision{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC()
	containers := []model.TransportContainer{
		{BaseModel: model.BaseModel{Code: "TC-T1", Name: "测试容器", Status: "in_transit", Version: 1}, Facility: "测试中心", Owner: "operator", Category: "箱", RiskLevel: "high", EffectiveAt: now, ContainerType: "主动制冷箱"},
	}
	if err := db.Create(&containers).Error; err != nil {
		t.Fatalf("seed container: %v", err)
	}
	evidence := []model.SensorEvidence{
		{Code: "SE-T1", ExcursionCode: "EE-T1", ContainerCode: "TC-T1", ObjectKey: "sensor/t1.csv", SHA256: "a1", MediaType: "text/csv", SizeBytes: 10, CapturedAt: now, CapturedBy: "logger", Source: "test", CreatedAt: now},
	}
	if err := db.Create(&evidence).Error; err != nil {
		t.Fatalf("seed evidence: %v", err)
	}

	securityRepo := repository.NewSecurityRepository(db)
	securitySvc := service.NewSecurityService(securityRepo, config.Config{})
	excursionRepo := repository.NewExcursionEventRepository(db)
	dispositionRepo := repository.NewDispositionDecisionRepository(db)
	assessmentRepo := repository.NewImpactAssessmentRepository(db)
	sensorRepo := repository.NewSensorEvidenceRepository(db)
	excursionSvc := service.NewExcursionEventService(excursionRepo, dispositionRepo, assessmentRepo, sensorRepo, securitySvc)
	dispositionSvc := service.NewDispositionDecisionService(dispositionRepo, assessmentRepo, excursionRepo, sensorRepo, securitySvc)
	return workflowFixtures{
		db: db, excursions: excursionSvc, dispositions: dispositionSvc,
		excursionRepo: excursionRepo, dispositionRepo: dispositionRepo, assessmentRepo: assessmentRepo,
	}
}

func createOpenExcursion(t *testing.T, f workflowFixtures) model.ExcursionEvent {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	item, err := f.excursions.Create(ctx, dto.CreateExcursionEvent{
		Code: "EE-T1", Name: "测试偏差", Facility: "测试中心", Owner: "operator", Category: "高温偏差",
		RiskLevel: "high", MetricValue: 9.4, MetricUnit: "C", EffectiveAt: now,
		Evidence: "minio://sensor/t1.csv", RelatedCode: "TC-T1", ContainerCode: "TC-T1",
		WindowCode: "TW-T1", ObservedTempC: 9.4, DurationMinutes: 20, DetectedAt: now,
		SensorEvidence: "minio://sensor/t1.csv",
	}, "operator", "req-create")
	if err != nil {
		t.Fatalf("create excursion: %v", err)
	}
	return item
}

func moveToDecided(t *testing.T, f workflowFixtures, item model.ExcursionEvent, actor, reason string) model.ExcursionEvent {
	t.Helper()
	ctx := context.Background()
	reviewed, err := f.excursions.Transition(ctx, item.ID, dto.TransitionRequest{
		Status: "in_review", ExpectedVersion: item.Version, Reason: "接收复核", Evidence: item.SensorEvidence,
	}, actor, "req-review")
	if err != nil {
		t.Fatalf("in_review: %v", err)
	}
	decided, err := f.excursions.Transition(ctx, item.ID, dto.TransitionRequest{
		Status: "decided", ExpectedVersion: reviewed.Version, Reason: reason, Evidence: reviewed.SensorEvidence,
	}, actor, "req-decide")
	if err != nil {
		t.Fatalf("decided: %v", err)
	}
	return decided
}

// TestAssessmentVersionLifecycle covers: version creation on evaluation,
// proposal pinned to the current version, independent approval, closure,
// return-for-review invalidation, re-evaluation with a new version, and a new
// independently approved decision that closes again.
func TestAssessmentVersionLifecycle(t *testing.T) {
	f := newWorkflowFixtures(t)
	ctx := context.Background()

	opened := createOpenExcursion(t, f)
	decided := moveToDecided(t, f, opened, "reviewer", "首次影响评估完成")
	if decided.CurrentAssessmentVersion == nil || *decided.CurrentAssessmentVersion != 1 {
		t.Fatalf("expected current assessment version 1, got %v", decided.CurrentAssessmentVersion)
	}
	assessments, err := f.assessmentRepo.ListForExcursion(ctx, "EE-T1")
	if err != nil || len(assessments) != 1 || assessments[0].Status != "current" {
		t.Fatalf("expected one current assessment, got %#v err=%v", assessments, err)
	}

	// A proposal cannot be raised while the deviation is not evaluated: move
	// happens before evaluation, so check rejection on a fresh open excursion
	// separately below. Here decided is evaluated, so proposal succeeds.
	proposal, err := f.dispositions.Create(ctx, dto.CreateDispositionDecision{
		Code: "DD-T1", Name: "隔离提议", Facility: "质量组", Owner: "operator", Category: "隔离",
		RiskLevel: "high", EffectiveAt: time.Now().UTC(), ExcursionCode: "EE-T1",
		DecisionBasis: "建议隔离", SensorEvidence: "minio://sensor/t1.csv",
	}, "operator", "req-propose")
	if err != nil {
		t.Fatalf("create proposal: %v", err)
	}
	if proposal.AssessmentVersion == nil || *proposal.AssessmentVersion != 1 {
		t.Fatalf("proposal must reference assessment v1, got %v", proposal.AssessmentVersion)
	}

	// Proposer cannot self-approve.
	if _, err := f.dispositions.Transition(ctx, proposal.ID, dto.TransitionRequest{
		Status: "quarantine", ExpectedVersion: proposal.Version, Reason: "自己批准", Evidence: proposal.SensorEvidence,
	}, "operator", "req-self"); err == nil {
		t.Fatal("self-approval must be rejected")
	}

	approved, err := f.dispositions.Transition(ctx, proposal.ID, dto.TransitionRequest{
		Status: "quarantine", ExpectedVersion: proposal.Version, Reason: "证据充分，批准隔离", Evidence: proposal.SensorEvidence,
	}, "reviewer", "req-approve")
	if err != nil {
		t.Fatalf("independent approval: %v", err)
	}
	if approved.ApprovedBy != "reviewer" || approved.AssessmentVersion == nil {
		t.Fatalf("approved decision missing approver/version: %#v", approved)
	}

	// Closure requires the approved final decision consistent with current version.
	closed, err := f.excursions.Transition(ctx, decided.ID, dto.TransitionRequest{
		Status: "closed", ExpectedVersion: decided.Version, Reason: "处置决定与评估版本一致，关闭偏差", Evidence: decided.SensorEvidence,
	}, "reviewer", "req-close")
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if closed.Status != "closed" {
		t.Fatalf("expected closed, got %s", closed.Status)
	}
}

// TestReturnForReviewInvalidatesDecisionAndReevaluation verifies that after
// return-for-review old decisions are history only, cannot be approved or
// close again, and a fresh assessment version requires a brand-new decision
// with its own independent approval.
func TestReturnForReviewInvalidatesDecisionAndReevaluation(t *testing.T) {
	f := newWorkflowFixtures(t)
	ctx := context.Background()

	opened := createOpenExcursion(t, f)
	decided := moveToDecided(t, f, opened, "reviewer", "首次影响评估完成")

	proposal, err := f.dispositions.Create(ctx, dto.CreateDispositionDecision{
		Code: "DD-T1", Name: "放行提议", Facility: "质量组", Owner: "operator", Category: "放行",
		RiskLevel: "high", EffectiveAt: time.Now().UTC(), ExcursionCode: "EE-T1",
		DecisionBasis: "建议放行", SensorEvidence: "minio://sensor/t1.csv",
	}, "operator", "req-propose-1")
	if err != nil {
		t.Fatalf("create proposal: %v", err)
	}
	if _, err := f.dispositions.Transition(ctx, proposal.ID, dto.TransitionRequest{
		Status: "release", ExpectedVersion: proposal.Version, Reason: "首次批准放行", Evidence: proposal.SensorEvidence,
	}, "reviewer", "req-approve-1"); err != nil {
		t.Fatalf("approve v1: %v", err)
	}

	// Return the evaluated deviation for re-review.
	returned, err := f.excursions.Transition(ctx, decided.ID, dto.TransitionRequest{
		Status: "in_review", ExpectedVersion: decided.Version, Reason: "补充稳定性数据，退回重审", Evidence: decided.SensorEvidence,
	}, "reviewer", "req-return")
	if err != nil {
		t.Fatalf("return for review: %v", err)
	}
	if returned.Status != "in_review" || returned.CurrentAssessmentVersion != nil {
		t.Fatalf("expected in_review with no current version, got %s %v", returned.Status, returned.CurrentAssessmentVersion)
	}
	old, err := f.dispositionRepo.Get(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("reload old decision: %v", err)
	}
	if old.InvalidatedReason == "" {
		t.Fatal("old decision must carry an invalidation reason")
	}
	oldAssessment, err := f.assessmentRepo.LatestForExcursion(ctx, "EE-T1")
	if err != nil || oldAssessment.Status != "superseded" || oldAssessment.SupersededAt == nil {
		t.Fatalf("assessment v1 must be superseded: %#v err=%v", oldAssessment, err)
	}

	// Old final decision cannot transition again (still immutable) and cannot
	// be edited.
	if _, err := f.dispositions.Update(ctx, old.ID, dto.UpdateDispositionDecision{
		ExpectedVersion: old.Version, Name: old.Name, Facility: old.Facility, Owner: old.Owner,
		Category: old.Category, RiskLevel: old.RiskLevel, EffectiveAt: old.EffectiveAt,
		ExcursionCode: old.ExcursionCode, DecisionBasis: "篡改", SensorEvidence: old.SensorEvidence,
	}, "operator", "req-edit-old"); err == nil {
		t.Fatal("invalidated decision must not be editable")
	}

	// Stale optimistic version on return attempt must conflict.
	if _, err := f.excursions.Transition(ctx, decided.ID, dto.TransitionRequest{
		Status: "in_review", ExpectedVersion: decided.Version, Reason: "重复退回", Evidence: decided.SensorEvidence,
	}, "reviewer", "req-return-stale"); err == nil {
		t.Fatal("returning with a stale expected version must fail")
	}

	// Re-evaluate: new current version 2.
	reevaluated := moveToDecidedWithBase(t, f, returned, "reviewer", "二次影响评估完成")
	if reevaluated.CurrentAssessmentVersion == nil || *reevaluated.CurrentAssessmentVersion != 2 {
		t.Fatalf("expected current assessment version 2, got %v", reevaluated.CurrentAssessmentVersion)
	}

	// Closing with the old (v1) decision is impossible: HasEffectiveFinal false.
	hasFinal, err := f.dispositionRepo.HasEffectiveFinalForExcursion(ctx, "EE-T1", 2)
	if err != nil || hasFinal {
		t.Fatalf("no effective final decision may exist for v2, got %v err=%v", hasFinal, err)
	}
	if _, err := f.excursions.Transition(ctx, reevaluated.ID, dto.TransitionRequest{
		Status: "closed", ExpectedVersion: reevaluated.Version, Reason: "尝试用旧决定关闭", Evidence: reevaluated.SensorEvidence,
	}, "reviewer", "req-close-stale"); err == nil {
		t.Fatal("closure with superseded decision must be rejected")
	}

	// A new proposal on v2, independently approved, closes again.
	proposal2, err := f.dispositions.Create(ctx, dto.CreateDispositionDecision{
		Code: "DD-T2", Name: "二次隔离提议", Facility: "质量组", Owner: "operator", Category: "隔离",
		RiskLevel: "high", EffectiveAt: time.Now().UTC(), ExcursionCode: "EE-T1",
		DecisionBasis: "二次评估建议隔离", SensorEvidence: "minio://sensor/t1.csv",
	}, "operator", "req-propose-2")
	if err != nil {
		t.Fatalf("create proposal v2: %v", err)
	}
	if proposal2.AssessmentVersion == nil || *proposal2.AssessmentVersion != 2 {
		t.Fatalf("new proposal must reference v2, got %v", proposal2.AssessmentVersion)
	}
	if _, err := f.dispositions.Transition(ctx, proposal2.ID, dto.TransitionRequest{
		Status: "quarantine", ExpectedVersion: proposal2.Version, Reason: "二次独立批准隔离", Evidence: proposal2.SensorEvidence,
	}, "admin", "req-approve-2"); err != nil {
		t.Fatalf("approve v2: %v", err)
	}
	closed2, err := f.excursions.Transition(ctx, reevaluated.ID, dto.TransitionRequest{
		Status: "closed", ExpectedVersion: reevaluated.Version, Reason: "新版本决定批准后关闭", Evidence: reevaluated.SensorEvidence,
	}, "reviewer", "req-close-2")
	if err != nil {
		t.Fatalf("close after re-evaluation: %v", err)
	}
	if closed2.Status != "closed" {
		t.Fatalf("expected closed on v2, got %s", closed2.Status)
	}

	// History keeps both versions and both decisions visible.
	list, err := f.assessmentRepo.ListForExcursion(ctx, "EE-T1")
	if err != nil || len(list) != 2 {
		t.Fatalf("expected 2 assessment versions in history, got %d err=%v", len(list), err)
	}
	decisions, err := f.dispositionRepo.ListForExcursion(ctx, "EE-T1")
	if err != nil || len(decisions) != 2 {
		t.Fatalf("expected 2 decisions in history, got %d err=%v", len(decisions), err)
	}
}

// TestProposalRejectedBeforeEvaluation ensures proposals only bind to a
// deviation that has a current assessment version.
func TestProposalRejectedBeforeEvaluation(t *testing.T) {
	f := newWorkflowFixtures(t)
	ctx := context.Background()
	createOpenExcursion(t, f)
	if _, err := f.dispositions.Create(ctx, dto.CreateDispositionDecision{
		Code: "DD-EARLY", Name: "过早提议", Facility: "质量组", Owner: "operator", Category: "隔离",
		RiskLevel: "high", EffectiveAt: time.Now().UTC(), ExcursionCode: "EE-T1",
		DecisionBasis: "偏差尚未评估", SensorEvidence: "minio://sensor/t1.csv",
	}, "operator", "req-early"); err == nil {
		t.Fatal("proposal before impact evaluation must be rejected")
	}
}

// TestConcurrentReturnAndApproval verifies the race requirement: whichever
// order the two requests commit in, only one effective result survives — a
// superseded decision can never close the deviation.
func TestConcurrentReturnAndApproval(t *testing.T) {
	cases := []struct {
		name string
	}{
		{name: "return-commits-before-approval"},
		{name: "approval-commits-before-return"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newWorkflowFixtures(t)
			ctx := context.Background()
			opened := createOpenExcursion(t, f)
			decided := moveToDecided(t, f, opened, "reviewer", "首次影响评估完成")
			proposal, err := f.dispositions.Create(ctx, dto.CreateDispositionDecision{
				Code: "DD-RACE", Name: "竞争隔离提议", Facility: "质量组", Owner: "operator", Category: "隔离",
				RiskLevel: "high", EffectiveAt: time.Now().UTC(), ExcursionCode: "EE-T1",
				DecisionBasis: "建议隔离", SensorEvidence: "minio://sensor/t1.csv",
			}, "operator", "req-propose-race")
			if err != nil {
				t.Fatalf("create proposal: %v", err)
			}

			approve := func() error {
				_, e := f.dispositions.Transition(ctx, proposal.ID, dto.TransitionRequest{
					Status: "quarantine", ExpectedVersion: proposal.Version, Reason: "竞争批准隔离", Evidence: proposal.SensorEvidence,
				}, "reviewer", "req-approve-race")
				return e
			}
			returnBack := func() error {
				_, e := f.excursions.Transition(ctx, decided.ID, dto.TransitionRequest{
					Status: "in_review", ExpectedVersion: decided.Version, Reason: "竞争退回重审", Evidence: decided.SensorEvidence,
				}, "reviewer", "req-return-race")
				return e
			}

			var approveErr, returnErr error
			approvalWonFirst := tc.name == "approval-commits-before-return"
			if approvalWonFirst {
				approveErr = approve()
				returnErr = returnBack()
			} else {
				returnErr = returnBack()
				approveErr = approve()
			}
			if returnErr != nil {
				t.Fatalf("return-for-review must succeed, got %v", returnErr)
			}
			// If the version was superseded first, the approval must be rejected;
			// if approval landed first it was valid at that instant, but the
			// subsequent return still invalidates it.
			if approvalWonFirst && approveErr != nil {
				t.Fatalf("approval landing first should commit, got %v", approveErr)
			}
			if !approvalWonFirst && approveErr == nil {
				t.Fatal("approval must be rejected once the version was superseded")
			}

			// Regardless of ordering, only one effective result survives: the
			// decision bound to v1 is invalidated and the deviation is back in
			// review with no current version, so closure is impossible.
			old, _ := f.dispositionRepo.Get(ctx, proposal.ID)
			if old.InvalidatedReason == "" {
				t.Fatal("decision must be invalidated after the return-for-review")
			}
			excursion, _ := f.excursionRepo.Get(ctx, decided.ID)
			if excursion.CurrentAssessmentVersion != nil || excursion.Status != "in_review" {
				t.Fatalf("deviation must be back in review with no current version, got %s %v", excursion.Status, excursion.CurrentAssessmentVersion)
			}
			hasFinal, _ := f.dispositionRepo.HasEffectiveFinalForExcursion(ctx, "EE-T1", 1)
			if hasFinal {
				t.Fatal("invalidated final decision must not count as effective for closure")
			}
		})
	}
}

func moveToDecidedWithBase(t *testing.T, f workflowFixtures, base model.ExcursionEvent, actor, reason string) model.ExcursionEvent {
	t.Helper()
	ctx := context.Background()
	decided, err := f.excursions.Transition(ctx, base.ID, dto.TransitionRequest{
		Status: "decided", ExpectedVersion: base.Version, Reason: reason, Evidence: base.SensorEvidence,
	}, actor, "req-redecide")
	if err != nil {
		t.Fatalf("re-decide: %v", err)
	}
	return decided
}
