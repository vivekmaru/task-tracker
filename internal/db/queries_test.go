package db

import "testing"

func TestSQLCGeneratedQueriesExposePhaseOneCore(t *testing.T) {
	var q *Queries

	_ = q.CreateWorkspace
	_ = q.CreateProject
	_ = q.CreateTicket
	_ = q.GetTicket
	_ = q.UpdateTicket
	_ = q.TransitionTicket
	_ = q.ListTickets
	_ = q.CreateTicketDependency
	_ = q.ListTicketDependencies
	_ = q.CreateAttempt
	_ = q.GetAttempt
	_ = q.ListAttemptsByTicket
	_ = q.ListExpiredRunningAttempts
	_ = q.ClaimNextTicket
	_ = q.HeartbeatAttempt
	_ = q.CheckpointAttempt
	_ = q.CompleteAttempt
	_ = q.FailAttempt
	_ = q.BlockAttempt
	_ = q.CancelAttempt
	_ = q.ExpireAttempt
	_ = q.CreateAttemptCheckpoint
	_ = q.ListAttemptCheckpointsByAttempt
	_ = q.ListAttemptCheckpointsByTicket
	_ = q.CreateTicketEvent
	_ = q.ListTicketEventsByTicket
	_ = q.ListRecentTicketEvents
	_ = q.ListTicketEventsAfterCursor
	_ = q.CreateIdempotencyKey
	_ = q.GetIdempotencyKey
	_ = q.DeleteExpiredIdempotencyKeys
	_ = q.CreateAttemptMetrics
	_ = q.GetAttemptMetrics
	_ = q.CreateArtifact
	_ = q.ListArtifactsByTicket
	_ = q.ListArtifactsByAttempt
	_ = q.ListArtifactsByScope
	_ = q.GetArtifact
	_ = q.DeleteArtifact
	_ = q.SearchTickets
	_ = q.UpsertWorkspaceMember
	_ = q.ListWorkspaceMembers
	_ = q.UpdateWorkspaceMemberRole
	_ = q.DeleteWorkspaceMember
}
