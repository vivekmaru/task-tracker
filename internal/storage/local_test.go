package storage

import (
	"context"
	"io"
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
	data, err := io.ReadAll(content.Reader)
	if err != nil {
		t.Fatalf("read artifact stream: %v", err)
	}
	if err := content.Reader.Close(); err != nil {
		t.Fatalf("close artifact stream: %v", err)
	}
	if string(data) != "ok\n" {
		t.Fatalf("unexpected content: %q", string(data))
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
	if !strings.HasPrefix(stored.URL, "local://artifacts/") || !strings.HasSuffix(stored.URL, "proof.log") || stored.Size != 6 || stored.MimeType == "" {
		t.Fatalf("unexpected stored metadata: %#v", stored)
	}
	relative, err := LocalRelativePath(stored.URL)
	if err != nil {
		t.Fatalf("stored URL should be locally readable: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatalf("read stored file: %v", err)
	}
	if string(data) != "proof\n" {
		t.Fatalf("unexpected stored content: %q", string(data))
	}
}

func TestLocalStoreUsesUniquePathsForSameFilename(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(t.TempDir(), "proof.log")
	second := filepath.Join(t.TempDir(), "proof.log")
	if err := os.WriteFile(first, []byte("first\n"), 0o600); err != nil {
		t.Fatalf("write first source: %v", err)
	}
	if err := os.WriteFile(second, []byte("second\n"), 0o600); err != nil {
		t.Fatalf("write second source: %v", err)
	}
	store := NewLocalStore(root)

	firstStored, err := store.StoreFile(context.Background(), first, "proof.log")
	if err != nil {
		t.Fatalf("store first file: %v", err)
	}
	secondStored, err := store.StoreFile(context.Background(), second, "proof.log")
	if err != nil {
		t.Fatalf("store second file: %v", err)
	}

	if firstStored.URL == secondStored.URL {
		t.Fatalf("expected unique stored URLs, got %q", firstStored.URL)
	}
	firstContent, err := store.Open(context.Background(), db.Artifact{Name: firstStored.Name, Url: firstStored.URL})
	if err != nil {
		t.Fatalf("open first artifact: %v", err)
	}
	secondContent, err := store.Open(context.Background(), db.Artifact{Name: secondStored.Name, Url: secondStored.URL})
	if err != nil {
		t.Fatalf("open second artifact: %v", err)
	}
	firstData, err := io.ReadAll(firstContent.Reader)
	if err != nil {
		t.Fatalf("read first artifact: %v", err)
	}
	if err := firstContent.Reader.Close(); err != nil {
		t.Fatalf("close first artifact: %v", err)
	}
	secondData, err := io.ReadAll(secondContent.Reader)
	if err != nil {
		t.Fatalf("read second artifact: %v", err)
	}
	if err := secondContent.Reader.Close(); err != nil {
		t.Fatalf("close second artifact: %v", err)
	}
	if string(firstData) != "first\n" || string(secondData) != "second\n" {
		t.Fatalf("stored content was overwritten: first=%q second=%q", firstData, secondData)
	}
}

func TestLocalStoreRejectsSymlinkEscapeOnOpen(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret\n"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "proof.log")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	store := NewLocalStore(root)

	_, err := store.Open(context.Background(), db.Artifact{
		Name: "proof.log",
		Url:  "local://artifacts/proof.log",
	})

	if err == nil || !strings.Contains(err.Error(), "escapes artifact root") {
		t.Fatalf("expected symlink escape rejection, got %v", err)
	}
}

func TestLocalStoreRejectsSymlinkEscapeOnWrite(t *testing.T) {
	root := t.TempDir()
	outsideDir := t.TempDir()
	if err := os.Symlink(outsideDir, filepath.Join(root, "linked")); err != nil {
		t.Fatalf("create symlinked parent: %v", err)
	}
	source := filepath.Join(t.TempDir(), "proof.log")
	if err := os.WriteFile(source, []byte("proof\n"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	store := NewLocalStore(root)

	_, err := store.StoreFile(context.Background(), source, "linked/proof.log")

	if err == nil || !strings.Contains(err.Error(), "escapes artifact root") {
		t.Fatalf("expected symlink write rejection, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(outsideDir, "proof.log")); err == nil {
		t.Fatal("outside symlink target was written")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat outside target: %v", err)
	}
}

func testUUID(seed byte) pgtype.UUID {
	var bytes [16]byte
	bytes[15] = seed
	return pgtype.UUID{Bytes: bytes, Valid: true}
}
