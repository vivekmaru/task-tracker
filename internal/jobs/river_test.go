package jobs

import (
	"testing"

	"github.com/riverqueue/river"
)

func TestMaintenanceArgsKind(t *testing.T) {
	if got := (MaintenanceArgs{}).Kind(); got != MaintenanceJobKind {
		t.Fatalf("expected maintenance kind %q, got %q", MaintenanceJobKind, got)
	}
}

func TestMaintenanceRiverWorkerImplementsRiverWorker(t *testing.T) {
	var _ river.Worker[MaintenanceArgs] = NewMaintenanceRiverWorker(NewMaintenanceWorker(&fakeMaintenanceStore{}))
}
