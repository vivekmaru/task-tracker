package runtime

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/vivek/agent-task-tracker/internal/db"
	"github.com/vivek/agent-task-tracker/internal/services"
	"github.com/vivek/agent-task-tracker/internal/storage"
)

func TestNewComposesQueriesServicesAndWorkers(t *testing.T) {
	queries := db.New(nil)

	rt := New(queries)

	if rt.Queries != queries {
		t.Fatalf("expected runtime to keep queries")
	}
	if rt.Tickets == nil {
		t.Fatal("expected ticket service")
	}
	if rt.Claims == nil {
		t.Fatal("expected claim service")
	}
	if rt.Attempts == nil {
		t.Fatal("expected attempt service")
	}
	if rt.Maintenance == nil {
		t.Fatal("expected maintenance worker")
	}
	if rt.Artifacts == nil {
		t.Fatal("expected artifact service")
	}
	if rt.Analytics == nil {
		t.Fatal("expected analytics service")
	}
	if rt.ArtifactStore == nil || rt.LocalStore == nil {
		t.Fatal("expected local artifact store")
	}
}

func TestRuntimeOpensS3ArtifactsWhenConfigured(t *testing.T) {
	client := &fakeS3Client{objects: map[string][]byte{"proof.log": []byte("proof\n")}}
	store, err := storage.NewS3Store(client, storage.S3Options{Bucket: "forge-artifacts"})
	if err != nil {
		t.Fatalf("new s3 store: %v", err)
	}
	rt := &Runtime{S3Store: store}

	content, err := rt.OpenArtifact(context.Background(), db.Artifact{
		StorageBackend: services.ArtifactStorageS3,
		Name:           "proof.log",
		Url:            "s3://forge-artifacts/proof.log",
	})

	if err != nil {
		t.Fatalf("open s3 artifact: %v", err)
	}
	data, err := io.ReadAll(content.Reader)
	if err != nil {
		t.Fatalf("read s3 artifact: %v", err)
	}
	if err := content.Reader.Close(); err != nil {
		t.Fatalf("close s3 artifact: %v", err)
	}
	if string(data) != "proof\n" {
		t.Fatalf("unexpected content: %q", data)
	}
}

func TestRuntimeReportsS3ArtifactOpenability(t *testing.T) {
	store, err := storage.NewS3Store(&fakeS3Client{}, storage.S3Options{Bucket: "forge-artifacts", Prefix: "proofs"})
	if err != nil {
		t.Fatalf("new s3 store: %v", err)
	}
	rt := &Runtime{S3Store: store}

	if !rt.ArtifactContentOpenable(db.Artifact{StorageBackend: services.ArtifactStorageS3, Url: "s3://forge-artifacts/proofs/proof.log"}) {
		t.Fatal("expected configured s3 bucket to be openable")
	}
	if rt.ArtifactContentOpenable(db.Artifact{StorageBackend: services.ArtifactStorageS3, Url: "s3://forge-artifacts/other/proof.log"}) {
		t.Fatal("expected different s3 prefix to be hidden")
	}
	if rt.ArtifactContentOpenable(db.Artifact{StorageBackend: services.ArtifactStorageS3, Url: "s3://other-bucket/proof.log"}) {
		t.Fatal("expected different s3 bucket to be hidden")
	}
	if (&Runtime{}).ArtifactContentOpenable(db.Artifact{StorageBackend: services.ArtifactStorageS3, Url: "s3://forge-artifacts/proof.log"}) {
		t.Fatal("expected unconfigured s3 runtime to hide s3 content")
	}
}

func TestRuntimeStoreArtifactUsesConfiguredStore(t *testing.T) {
	source := t.TempDir() + "/proof.log"
	if err := os.WriteFile(source, []byte("proof\n"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	client := &fakeS3Client{}
	store, err := storage.NewS3Store(client, storage.S3Options{Bucket: "forge-artifacts"})
	if err != nil {
		t.Fatalf("new s3 store: %v", err)
	}
	rt := &Runtime{S3Store: store, ArtifactStore: store}

	stored, err := rt.StoreArtifact(context.Background(), source, "proof.log")

	if err != nil {
		t.Fatalf("store artifact: %v", err)
	}
	if stored.StorageBackend != services.ArtifactStorageS3 || stored.URL == "" {
		t.Fatalf("expected s3 stored artifact, got %#v", stored)
	}
	if len(client.objects) != 1 {
		t.Fatalf("expected one stored object, got %#v", client.objects)
	}
}

type fakeS3Client struct {
	objects map[string][]byte
}

func (f *fakeS3Client) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if f.objects == nil {
		f.objects = map[string][]byte{}
	}
	data, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	f.objects[aws.ToString(input.Key)] = data
	return &s3.PutObjectOutput{}, nil
}

func (f *fakeS3Client) GetObject(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	data := f.objects[aws.ToString(input.Key)]
	return &s3.GetObjectOutput{
		Body:          io.NopCloser(bytes.NewReader(data)),
		ContentLength: aws.Int64(int64(len(data))),
	}, nil
}

func (f *fakeS3Client) DeleteObject(_ context.Context, input *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	delete(f.objects, aws.ToString(input.Key))
	return &s3.DeleteObjectOutput{}, nil
}
