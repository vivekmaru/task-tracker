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

type WebhookDeliveryArgs struct{}

func (WebhookDeliveryArgs) Kind() string {
	return WebhookDeliveryJobKind
}

type WebhookDeliveryRiverWorker struct {
	river.WorkerDefaults[WebhookDeliveryArgs]

	Worker *WebhookWorker
}

var _ river.Worker[WebhookDeliveryArgs] = (*WebhookDeliveryRiverWorker)(nil)

func NewWebhookDeliveryRiverWorker(worker *WebhookWorker) *WebhookDeliveryRiverWorker {
	return &WebhookDeliveryRiverWorker{Worker: worker}
}

func (w *WebhookDeliveryRiverWorker) Work(ctx context.Context, _ *river.Job[WebhookDeliveryArgs]) error {
	_, err := w.Worker.RunOnce(ctx)
	return err
}

func RegisterWebhookDeliveryWorker(workers *river.Workers, worker *WebhookWorker) {
	river.AddWorker(workers, NewWebhookDeliveryRiverWorker(worker))
}
