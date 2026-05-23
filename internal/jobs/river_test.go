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

func TestWebhookDeliveryArgsKind(t *testing.T) {
	if got := (WebhookDeliveryArgs{}).Kind(); got != WebhookDeliveryJobKind {
		t.Fatalf("expected webhook delivery kind %q, got %q", WebhookDeliveryJobKind, got)
	}
}

func TestWebhookDeliveryRiverWorkerImplementsRiverWorker(t *testing.T) {
	var _ river.Worker[WebhookDeliveryArgs] = NewWebhookDeliveryRiverWorker(NewWebhookWorker(&fakeWebhookStore{}))
}
