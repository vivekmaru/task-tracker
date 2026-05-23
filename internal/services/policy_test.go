package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPolicyAllowsPlainHumanTicketCreation(t *testing.T) {
	policy := NewPolicyService(PolicyConfig{})

	decision := policy.Evaluate(PolicyInput{
		Operation:            PolicyOperationCreateTicket,
		ActorType:            ActorHuman,
		ActorID:              "vivek",
		AcceptanceCriteria:   []string{"Tests pass"},
		VerificationCommands: []string{"go test ./..."},
	})

	if decision.Decision != PolicyDecisionAllow {
		t.Fatalf("expected allow, got %#v", decision)
	}
}

func TestPolicyWarnsForThinAgentCreatedTicket(t *testing.T) {
	policy := NewPolicyService(PolicyConfig{})

	decision := policy.Evaluate(PolicyInput{
		Operation: PolicyOperationProposeTicket,
		ActorType: ActorAgent,
		ActorID:   "codex",
	})

	if decision.Decision != PolicyDecisionWarn {
		t.Fatalf("expected warn, got %#v", decision)
	}
	for _, want := range []string{
		"agent-created work should preserve source attempt or artifact attribution",
		"agent-created work should include a creation reason",
		"ticket has no acceptance criteria",
		"ticket has no verification commands",
	} {
		if !containsString(decision.Reasons, want) {
			t.Fatalf("expected warning %q in %#v", want, decision.Reasons)
		}
	}
}

func TestPolicyDeniesAgentEnqueueWithoutAuthority(t *testing.T) {
	policy := NewPolicyService(PolicyConfig{})

	decision := policy.Evaluate(PolicyInput{
		Operation:            PolicyOperationCreateTicket,
		ActorType:            ActorAgent,
		ActorID:              "codex",
		AcceptanceCriteria:   []string{"Follow-up is actionable"},
		VerificationCommands: []string{"go test ./..."},
		HasSourceAttempt:     true,
		CreationReason:       "Found while implementing nearby work",
		Enqueue:              true,
	})

	if decision.Decision != PolicyDecisionDeny {
		t.Fatalf("expected deny, got %#v", decision)
	}
	if !errors.Is(decision.Error(), ErrPolicyDenied) {
		t.Fatalf("expected policy denied error, got %v", decision.Error())
	}
}

func TestPolicyEvaluatesClaimTicketContext(t *testing.T) {
	policy := NewPolicyService(PolicyConfig{MaxClaimLease: time.Hour})

	decision := policy.Evaluate(PolicyInput{
		Operation:            PolicyOperationClaimNextTicket,
		ActorType:            ActorAgent,
		ActorID:              "codex",
		Harness:              "codex",
		AllowedHarnesses:     []string{"codex"},
		AgentCapabilities:    []string{"codegen", "testing"},
		RequiredCapabilities: []string{"testing"},
		RequiredPermissions:  []string{"filesystem"},
		RequiredTools:        []string{"go"},
		Lease:                30 * time.Minute,
	})

	if decision.Decision != PolicyDecisionWarn {
		t.Fatalf("expected warnings for permissions/tools, got %#v", decision)
	}
	if joined := strings.Join(decision.Reasons, "\n"); !strings.Contains(joined, "ticket requires permissions: filesystem") || !strings.Contains(joined, "ticket requires tools: go") {
		t.Fatalf("expected permission/tool warnings, got %#v", decision.Reasons)
	}
}

func TestPolicyDeniesClaimWhenHarnessOrCapabilitiesDoNotMatch(t *testing.T) {
	policy := NewPolicyService(PolicyConfig{MaxClaimLease: time.Hour})

	decision := policy.Evaluate(PolicyInput{
		Operation:            PolicyOperationClaimNextTicket,
		ActorType:            ActorAgent,
		ActorID:              "codex",
		Harness:              "opencode",
		AllowedHarnesses:     []string{"codex"},
		AgentCapabilities:    []string{"codegen"},
		RequiredCapabilities: []string{"testing"},
		Lease:                90 * time.Minute,
	})

	if decision.Decision != PolicyDecisionDeny {
		t.Fatalf("expected deny, got %#v", decision)
	}
	for _, want := range []string{
		`harness "opencode" is not allowed for this ticket`,
		"agent is missing required capabilities: testing",
		"claim lease 1h30m0s exceeds maximum 1h0m0s",
	} {
		if !containsString(decision.Reasons, want) {
			t.Fatalf("expected deny reason %q in %#v", want, decision.Reasons)
		}
	}
}

func TestPolicyDefaultClaimLeaseMatchesContractLimit(t *testing.T) {
	policy := NewPolicyService(PolicyConfig{})

	decision := policy.Evaluate(PolicyInput{
		Operation: PolicyOperationClaimNextTicket,
		ActorType: ActorAgent,
		ActorID:   "codex",
		Harness:   "codex",
		Lease:     24 * time.Hour,
	})

	if decision.Decision == PolicyDecisionDeny {
		t.Fatalf("expected published 24h lease to be accepted, got %#v", decision)
	}
}

func TestPolicyDeniesNonRunningAttemptTransition(t *testing.T) {
	policy := NewPolicyService(PolicyConfig{})

	decision := policy.Evaluate(PolicyInput{
		Operation:     PolicyOperationCompleteAttempt,
		ActorType:     ActorAgent,
		ActorID:       "codex",
		AttemptStatus: AttemptStatusBlocked,
	})

	if decision.Decision != PolicyDecisionDeny {
		t.Fatalf("expected deny, got %#v", decision)
	}
	if !containsString(decision.Reasons, `cannot transition an attempt with status "blocked"`) {
		t.Fatalf("expected non-running attempt reason, got %#v", decision.Reasons)
	}
}

func TestClaimNextUsesPolicyGuardrailForLease(t *testing.T) {
	service := NewClaimService(&fakeClaimStore{}, WithClaimPolicy(NewPolicyService(PolicyConfig{
		MaxClaimLease: time.Hour,
	})))

	_, err := service.ClaimNext(context.Background(), ClaimNextRequest{
		WorkspaceID: testUUID(1),
		ProjectID:   testUUID(2),
		Harness:     "codex",
		AgentID:     "codex",
		Lease:       2 * time.Hour,
	})
	if !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("expected policy denied error, got %v", err)
	}
}
