package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

func TestLocalStoreOpensArtifactUnderRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go-test.log"), []byte("ok\n"), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	store := NewLocalStore(root)

	content, err := store.Open(context.Background(), db.Artifact{
		ID:       testUUID(1),
		Name:     "go-test.log",
		Url:      "local://artifacts/go-test.log",
		MimeType: "text/plain",
	})

	if err != nil {
		t.Fatalf("open artifact: %v", err)
	}
	if string(content.Data) != "ok\n" {
		t.Fatalf("unexpected content: %q", string(content.Data))
	}
	if content.Name != "go-test.log" || content.MimeType != "text/plain" || content.Size != 3 {
		t.Fatalf("unexpected content metadata: %#v", content)
	}
}

func TestLocalStoreRejectsTraversal(t *testing.T) {
	store := NewLocalStore(t.TempDir())

	_, err := store.Open(context.Background(), db.Artifact{
		ID:  testUUID(1),
		Url: "local://artifacts/../secret.txt",
	})

	if err == nil || !strings.Contains(err.Error(), "invalid local artifact URL") {
		t.Fatalf("expected traversal rejection, got %v", err)
	}
}

func TestLocalStoreCopiesFilesIntoArtifactRoot(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "proof.log")
	if err := os.WriteFile(source, []byte("proof\n"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	store := NewLocalStore(root)

	stored, err := store.StoreFile(context.Background(), source, "proof.log")

	if err != nil {
		t.Fatalf("store file: %v", err)
	}
	if stored.URL != "local://artifacts/proof.log" || stored.Size != 6 || stored.MimeType == "" {
		t.Fatalf("unexpected stored metadata: %#v", stored)
	}
	data, err := os.ReadFile(filepath.Join(root, "proof.log"))
	if err != nil {
		t.Fatalf("read stored file: %v", err)
	}
	if string(data) != "proof\n" {
		t.Fatalf("unexpected stored content: %q", string(data))
	}
}

func testUUID(seed byte) pgtype.UUID {
	var bytes [16]byte
	bytes[15] = seed
	return pgtype.UUID{Bytes: bytes, Valid: true}
}
