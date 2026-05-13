package jobs

import (
	"context"

	"github.com/riverqueue/river"
)

const MaintenanceJobKind = "forge_maintenance"

type MaintenanceArgs struct{}

func (MaintenanceArgs) Kind() string {
	return MaintenanceJobKind
}

type MaintenanceRiverWorker struct {
	river.WorkerDefaults[MaintenanceArgs]

	Worker *MaintenanceWorker
}

var _ river.Worker[MaintenanceArgs] = (*MaintenanceRiverWorker)(nil)

func NewMaintenanceRiverWorker(worker *MaintenanceWorker) *MaintenanceRiverWorker {
	return &MaintenanceRiverWorker{Worker: worker}
}

func (w *MaintenanceRiverWorker) Work(ctx context.Context, _ *river.Job[MaintenanceArgs]) error {
	_, err := w.Worker.RunOnce(ctx)
	return err
}

func RegisterMaintenanceWorker(workers *river.Workers, worker *MaintenanceWorker) {
	river.AddWorker(workers, NewMaintenanceRiverWorker(worker))
}
