package storage

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/vivek/agent-task-tracker/internal/db"
)

func TestS3StoreCopiesFilesIntoConfiguredBucket(t *testing.T) {
	source := filepath.Join(t.TempDir(), "proof.log")
	if err := os.WriteFile(source, []byte("proof\n"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	client := &fakeS3Client{}
	store, err := NewS3Store(client, S3Options{Bucket: "forge-artifacts", Prefix: "proofs"})
	if err != nil {
		t.Fatalf("new s3 store: %v", err)
	}

	stored, err := store.StoreFile(context.Background(), source, "proof.log")

	if err != nil {
		t.Fatalf("store file: %v", err)
	}
	if stored.StorageBackend != BackendS3 || stored.Size != 6 || stored.MimeType == "" {
		t.Fatalf("unexpected stored metadata: %#v", stored)
	}
	bucket, key, err := S3ObjectLocation(stored.URL)
	if err != nil {
		t.Fatalf("stored URL should be readable: %v", err)
	}
	if bucket != "forge-artifacts" || !strings.HasPrefix(key, "proofs/") || !strings.HasSuffix(key, "proof.log") {
		t.Fatalf("unexpected s3 location: bucket=%q key=%q", bucket, key)
	}
	if string(client.objects[key]) != "proof\n" {
		t.Fatalf("unexpected stored content: %q", client.objects[key])
	}
}

func TestS3StoreOpensArtifacts(t *testing.T) {
	client := &fakeS3Client{
		objects:      map[string][]byte{"proofs/proof.log": []byte("ok\n")},
		contentTypes: map[string]string{"proofs/proof.log": "text/plain"},
	}
	store, err := NewS3Store(client, S3Options{Bucket: "forge-artifacts", Prefix: "proofs"})
	if err != nil {
		t.Fatalf("new s3 store: %v", err)
	}

	content, err := store.Open(context.Background(), db.Artifact{
		Name:     "proof.log",
		Url:      "s3://forge-artifacts/proofs/proof.log",
		MimeType: "",
	})

	if err != nil {
		t.Fatalf("open artifact: %v", err)
	}
	data, err := io.ReadAll(content.Reader)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	if err := content.Reader.Close(); err != nil {
		t.Fatalf("close artifact: %v", err)
	}
	if string(data) != "ok\n" || content.MimeType != "text/plain" || content.Size != 3 {
		t.Fatalf("unexpected artifact content: data=%q metadata=%#v", data, content)
	}
}

func TestS3StoreRejectsBucketMismatch(t *testing.T) {
	store, err := NewS3Store(&fakeS3Client{}, S3Options{Bucket: "forge-artifacts"})
	if err != nil {
		t.Fatalf("new s3 store: %v", err)
	}

	_, err = store.Open(context.Background(), db.Artifact{Url: "s3://other-bucket/proof.log"})

	if err == nil || !strings.Contains(err.Error(), "does not match configured bucket") {
		t.Fatalf("expected bucket mismatch, got %v", err)
	}
}

func TestS3ObjectLocationPreservesObjectKeys(t *testing.T) {
	bucket, key, err := S3ObjectLocation("s3://forge-artifacts/proofs//./..%2Fsecret%20proof.log")

	if err != nil {
		t.Fatalf("parse s3 artifact URL: %v", err)
	}
	if bucket != "forge-artifacts" || key != "proofs//./../secret proof.log" {
		t.Fatalf("expected exact key preservation, got bucket=%q key=%q", bucket, key)
	}
}

type fakeS3Client struct {
	objects      map[string][]byte
	contentTypes map[string]string
	deletedKey   string
}

func (f *fakeS3Client) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if f.objects == nil {
		f.objects = map[string][]byte{}
	}
	if f.contentTypes == nil {
		f.contentTypes = map[string]string{}
	}
	data, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	key := aws.ToString(input.Key)
	f.objects[key] = data
	f.contentTypes[key] = aws.ToString(input.ContentType)
	return &s3.PutObjectOutput{}, nil
}

func (f *fakeS3Client) GetObject(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	key := aws.ToString(input.Key)
	data := f.objects[key]
	return &s3.GetObjectOutput{
		Body:          io.NopCloser(bytes.NewReader(data)),
		ContentLength: aws.Int64(int64(len(data))),
		ContentType:   aws.String(f.contentTypes[key]),
	}, nil
}

func (f *fakeS3Client) DeleteObject(_ context.Context, input *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	f.deletedKey = aws.ToString(input.Key)
	delete(f.objects, f.deletedKey)
	return &s3.DeleteObjectOutput{}, nil
}
