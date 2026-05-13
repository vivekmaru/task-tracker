package db

import "testing"

func TestSQLCGeneratedQueriesExposePhaseOneCore(t *testing.T) {
	var q *Queries

	_ = q.CreateWorkspace
	_ = q.CreateProject
	_ = q.CreateTicket
	_ = q.GetTicket
	_ = q.ListTickets
	_ = q.CreateTicketDependency
	_ = q.ListTicketDependencies
	_ = q.CreateAttempt
	_ = q.GetAttempt
	_ = q.ListAttemptsByTicket
	_ = q.ClaimNextTicket
	_ = q.CreateAttemptCheckpoint
	_ = q.ListAttemptCheckpointsByAttempt
	_ = q.ListAttemptCheckpointsByTicket
	_ = q.CreateTicketEvent
	_ = q.ListTicketEventsByTicket
	_ = q.CreateIdempotencyKey
	_ = q.GetIdempotencyKey
	_ = q.DeleteExpiredIdempotencyKeys
	_ = q.CreateAttemptMetrics
	_ = q.GetAttemptMetrics
	_ = q.CreateArtifact
	_ = q.ListArtifactsByTicket
}
