package storage

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/vivek/agent-task-tracker/internal/db"
)

type S3Client interface {
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

type S3Store struct {
	client S3Client
	bucket string
	prefix string
}

type S3Options struct {
	Bucket string
	Prefix string
}

func NewS3Store(client S3Client, opts S3Options) (*S3Store, error) {
	bucket := strings.TrimSpace(opts.Bucket)
	if bucket == "" {
		return nil, errors.New("s3 bucket is required")
	}
	if client == nil {
		return nil, errors.New("s3 client is required")
	}
	prefix, err := cleanS3Prefix(opts.Prefix)
	if err != nil {
		return nil, err
	}
	return &S3Store{client: client, bucket: bucket, prefix: prefix}, nil
}

func (s *S3Store) Open(ctx context.Context, artifact db.Artifact) (ArtifactContent, error) {
	bucket, key, err := S3ObjectLocation(artifact.Url)
	if err != nil {
		return ArtifactContent{}, err
	}
	if bucket != s.bucket {
		return ArtifactContent{}, fmt.Errorf("s3 artifact bucket %q does not match configured bucket", bucket)
	}
	if !s.keyInScope(key) {
		return ArtifactContent{}, fmt.Errorf("s3 artifact key %q is outside configured prefix", key)
	}
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return ArtifactContent{}, fmt.Errorf("open s3 artifact: %w", err)
	}
	name := strings.TrimSpace(artifact.Name)
	if name == "" {
		name = path.Base(key)
	}
	mimeType := strings.TrimSpace(artifact.MimeType)
	if mimeType == "" {
		mimeType = strings.TrimSpace(aws.ToString(result.ContentType))
	}
	if mimeType == "" {
		mimeType = mime.TypeByExtension(filepath.Ext(name))
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return ArtifactContent{
		Name:     name,
		MimeType: mimeType,
		Size:     aws.ToInt64(result.ContentLength),
		Reader:   result.Body,
	}, nil
}

func (s *S3Store) StoreFile(ctx context.Context, sourcePath string, preferredName string) (StoredArtifact, error) {
	sourcePath = strings.TrimSpace(sourcePath)
	if sourcePath == "" {
		return StoredArtifact{}, errors.New("source path is required")
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		return StoredArtifact{}, fmt.Errorf("stat source artifact: %w", err)
	}
	if info.IsDir() {
		return StoredArtifact{}, errors.New("source artifact must be a file")
	}
	if err := requireRegularFile(info, "source artifact"); err != nil {
		return StoredArtifact{}, err
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return StoredArtifact{}, fmt.Errorf("open source artifact: %w", err)
	}
	defer source.Close()
	name, err := cleanArtifactName(firstNonEmpty(preferredName, filepath.Base(sourcePath)))
	if err != nil {
		return StoredArtifact{}, err
	}
	token, err := randomPathToken()
	if err != nil {
		return StoredArtifact{}, err
	}
	key := joinS3Key(s.prefix, uniqueArtifactPath(name, token))
	mimeType := mime.TypeByExtension(filepath.Ext(name))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	if _, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          source,
		ContentLength: aws.Int64(info.Size()),
		ContentType:   aws.String(mimeType),
	}); err != nil {
		return StoredArtifact{}, fmt.Errorf("store s3 artifact: %w", err)
	}
	return StoredArtifact{
		Name:           name,
		URL:            s3ArtifactURL(s.bucket, key),
		StorageBackend: BackendS3,
		MimeType:       mimeType,
		Size:           info.Size(),
	}, nil
}

func (s *S3Store) Remove(ctx context.Context, rawURL string) error {
	bucket, key, err := S3ObjectLocation(rawURL)
	if err != nil {
		return err
	}
	if bucket != s.bucket {
		return fmt.Errorf("s3 artifact bucket %q does not match configured bucket", bucket)
	}
	if !s.keyInScope(key) {
		return fmt.Errorf("s3 artifact key %q is outside configured prefix", key)
	}
	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}); err != nil {
		return fmt.Errorf("remove s3 artifact: %w", err)
	}
	return nil
}

func (s *S3Store) CanOpenURL(rawURL string) bool {
	bucket, key, err := S3ObjectLocation(rawURL)
	if err != nil || bucket != s.bucket {
		return false
	}
	return s.keyInScope(key)
}

func (s *S3Store) keyInScope(key string) bool {
	if s.prefix == "" {
		return true
	}
	return key == s.prefix || strings.HasPrefix(key, s.prefix+"/")
}

func S3ObjectLocation(rawURL string) (string, string, error) {
	rawURL = strings.TrimSpace(rawURL)
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", "", fmt.Errorf("invalid s3 artifact URL: %w", err)
	}
	if parsed.Scheme != "s3" || parsed.Host == "" {
		return "", "", fmt.Errorf("invalid s3 artifact URL %q", rawURL)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", fmt.Errorf("invalid s3 artifact URL %q", rawURL)
	}
	key, err := url.PathUnescape(strings.TrimPrefix(parsed.EscapedPath(), "/"))
	if err != nil {
		return "", "", fmt.Errorf("invalid s3 artifact URL: %w", err)
	}
	if key == "" {
		return "", "", fmt.Errorf("invalid s3 artifact URL %q", rawURL)
	}
	return parsed.Host, key, nil
}

func IsS3ArtifactURL(rawURL string) bool {
	_, _, err := S3ObjectLocation(rawURL)
	return err == nil
}

func cleanS3Prefix(prefix string) (string, error) {
	prefix = strings.Trim(strings.TrimSpace(strings.ReplaceAll(prefix, "\\", "/")), "/")
	if prefix == "" {
		return "", nil
	}
	cleaned, err := cleanArtifactName(prefix)
	if err != nil {
		return "", fmt.Errorf("invalid s3 prefix: %w", err)
	}
	return cleaned, nil
}

func joinS3Key(prefix string, name string) string {
	if strings.TrimSpace(prefix) == "" {
		return name
	}
	return path.Join(prefix, name)
}

func s3ArtifactURL(bucket string, key string) string {
	artifactURL := url.URL{
		Scheme: "s3",
		Host:   bucket,
		Path:   "/" + strings.ReplaceAll(key, "\\", "/"),
	}
	return artifactURL.String()
}
