package services

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	PolicyDecisionAllow = "allow"
	PolicyDecisionWarn  = "warn"
	PolicyDecisionDeny  = "deny"

	PolicyOperationCreateTicket      = "create_ticket"
	PolicyOperationProposeTicket     = "propose_ticket"
	PolicyOperationClaimNextTicket   = "claim_next_ticket"
	PolicyOperationCheckpointAttempt = "checkpoint_attempt"
	PolicyOperationCompleteAttempt   = "complete_attempt"
	PolicyOperationFailAttempt       = "fail_attempt"
	PolicyOperationBlockAttempt      = "block_attempt"
	PolicyOperationTicketTransition  = "ticket_transition"
)

var ErrPolicyDenied = errors.New("policy denied workflow action")

type PolicyService struct {
	config PolicyConfig
}

type PolicyConfig struct {
	MaxClaimLease time.Duration
}

type PolicyInput struct {
	Operation string

	ActorType string
	ActorID   string

	TicketStatus    string
	TicketType      string
	TicketCreatedBy string

	AttemptStatus string

	Harness              string
	AllowedHarnesses     []string
	AgentCapabilities    []string
	RequiredCapabilities []string
	RequiredPermissions  []string
	RequiredTools        []string

	AcceptanceCriteria   []string
	VerificationCommands []string
	CreationReason       string
	HasSourceAttempt     bool
	HasSourceArtifact    bool

	Enqueue    bool
	CanEnqueue bool
	Lease      time.Duration
}

type PolicyDecision struct {
	Decision string
	Reasons  []string
}

func NewPolicyService(config PolicyConfig) *PolicyService {
	if config.MaxClaimLease == 0 {
		config.MaxClaimLease = 2 * time.Hour
	}
	return &PolicyService{config: config}
}

func (s *PolicyService) Evaluate(input PolicyInput) PolicyDecision {
	input = normalizePolicyInput(input)

	var denies []string
	var warnings []string

	switch input.Operation {
	case PolicyOperationCreateTicket, PolicyOperationProposeTicket:
		evaluateTicketCreationPolicy(input, &denies, &warnings)
	case PolicyOperationClaimNextTicket:
		evaluateClaimPolicy(s.config, input, &denies, &warnings)
	case PolicyOperationCheckpointAttempt:
		evaluateRunningAttemptPolicy(input, &denies, &warnings, "checkpoint")
	case PolicyOperationCompleteAttempt, PolicyOperationFailAttempt, PolicyOperationBlockAttempt:
		evaluateRunningAttemptPolicy(input, &denies, &warnings, "transition")
	case PolicyOperationTicketTransition:
		evaluateTicketTransitionPolicy(input, &denies, &warnings)
	default:
		denies = append(denies, fmt.Sprintf("operation %q is not policy-managed", input.Operation))
	}

	if len(denies) > 0 {
		return PolicyDecision{Decision: PolicyDecisionDeny, Reasons: denies}
	}
	if len(warnings) > 0 {
		return PolicyDecision{Decision: PolicyDecisionWarn, Reasons: warnings}
	}
	return PolicyDecision{Decision: PolicyDecisionAllow, Reasons: []string{"no policy guardrails triggered"}}
}

func (d PolicyDecision) Allowed() bool {
	return d.Decision != PolicyDecisionDeny
}

func (d PolicyDecision) Error() error {
	if d.Allowed() {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrPolicyDenied, strings.Join(d.Reasons, "; "))
}

func evaluateTicketCreationPolicy(input PolicyInput, denies, warnings *[]string) {
	if input.ActorType == ActorAgent && input.Enqueue && !input.CanEnqueue {
		*denies = append(*denies, "agent-created work cannot be directly enqueued without enqueue authority")
	}
	if input.ActorType == ActorAgent && !input.HasSourceAttempt && !input.HasSourceArtifact {
		*warnings = append(*warnings, "agent-created work should preserve source attempt or artifact attribution")
	}
	if input.ActorType == ActorAgent && input.CreationReason == "" {
		*warnings = append(*warnings, "agent-created work should include a creation reason")
	}
	if len(input.AcceptanceCriteria) == 0 {
		*warnings = append(*warnings, "ticket has no acceptance criteria")
	}
	if len(input.VerificationCommands) == 0 {
		*warnings = append(*warnings, "ticket has no verification commands")
	}
}

func evaluateClaimPolicy(config PolicyConfig, input PolicyInput, denies, warnings *[]string) {
	if input.Lease > config.MaxClaimLease {
		*denies = append(*denies, fmt.Sprintf("claim lease %s exceeds maximum %s", input.Lease, config.MaxClaimLease))
	}
	if len(input.AllowedHarnesses) > 0 && !containsFold(input.AllowedHarnesses, input.Harness) {
		*denies = append(*denies, fmt.Sprintf("harness %q is not allowed for this ticket", input.Harness))
	}
	missingCapabilities := missingStrings(input.RequiredCapabilities, input.AgentCapabilities)
	if len(missingCapabilities) > 0 {
		*denies = append(*denies, "agent is missing required capabilities: "+strings.Join(missingCapabilities, ", "))
	}
	if len(input.RequiredPermissions) > 0 {
		*warnings = append(*warnings, "ticket requires permissions: "+strings.Join(input.RequiredPermissions, ", "))
	}
	if len(input.RequiredTools) > 0 {
		*warnings = append(*warnings, "ticket requires tools: "+strings.Join(input.RequiredTools, ", "))
	}
}

func evaluateRunningAttemptPolicy(input PolicyInput, denies, warnings *[]string, action string) {
	if input.AttemptStatus != "" && input.AttemptStatus != AttemptStatusRunning {
		*denies = append(*denies, fmt.Sprintf("cannot %s an attempt with status %q", action, input.AttemptStatus))
	}
	if action == "transition" && len(input.VerificationCommands) == 0 {
		*warnings = append(*warnings, "attempt transition has no verification commands on the ticket")
	}
}

func evaluateTicketTransitionPolicy(input PolicyInput, denies, warnings *[]string) {
	switch input.TicketStatus {
	case TicketStatusArchived:
		*denies = append(*denies, "archived tickets must be reopened before workflow transitions")
	case TicketStatusDone:
		*warnings = append(*warnings, "done tickets should only transition with an explicit reason")
	}
}

func normalizePolicyInput(input PolicyInput) PolicyInput {
	input.Operation = strings.TrimSpace(input.Operation)
	input.ActorType = strings.TrimSpace(input.ActorType)
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.TicketStatus = strings.TrimSpace(input.TicketStatus)
	input.TicketType = strings.TrimSpace(input.TicketType)
	input.TicketCreatedBy = strings.TrimSpace(input.TicketCreatedBy)
	input.AttemptStatus = strings.TrimSpace(input.AttemptStatus)
	input.Harness = strings.TrimSpace(input.Harness)
	input.CreationReason = strings.TrimSpace(input.CreationReason)
	input.AllowedHarnesses = compactStrings(input.AllowedHarnesses)
	input.AgentCapabilities = compactStrings(input.AgentCapabilities)
	input.RequiredCapabilities = compactStrings(input.RequiredCapabilities)
	input.RequiredPermissions = compactStrings(input.RequiredPermissions)
	input.RequiredTools = compactStrings(input.RequiredTools)
	input.AcceptanceCriteria = compactStrings(input.AcceptanceCriteria)
	input.VerificationCommands = compactStrings(input.VerificationCommands)
	return input
}

func missingStrings(required, actual []string) []string {
	var missing []string
	for _, requiredValue := range required {
		if !containsFold(actual, requiredValue) {
			missing = append(missing, requiredValue)
		}
	}
	return missing
}

func containsFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(value, want) {
			return true
		}
	}
	return false
}
