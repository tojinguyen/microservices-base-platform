package storage

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	transcoderConfig "github.com/tojinguyen/transcoder/internal/config"
)

type S3Storage struct {
	client *s3.Client
	bucket string
}

func NewS3Storage(cfg transcoderConfig.StorageConfig) (*S3Storage, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	clientOpts := []func(*s3.Options){
		func(o *s3.Options) { o.UsePathStyle = cfg.UsePathStyle },
	}
	if cfg.Endpoint != "" {
		clientOpts = append(clientOpts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		})
	}

	return &S3Storage{
		client: s3.NewFromConfig(awsCfg, clientOpts...),
		bucket: cfg.Bucket,
	}, nil
}

func (s *S3Storage) GetObject(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("get object %s: %w", key, err)
	}
	return out.Body, nil
}

func (s *S3Storage) PutObject(ctx context.Context, key, contentType string, body io.Reader, size int64) error {
	input := &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        body,
		ContentType: aws.String(contentType),
	}
	if size > 0 {
		input.ContentLength = aws.Int64(size)
	}
	_, err := s.client.PutObject(ctx, input)
	if err != nil {
		return fmt.Errorf("put object %s: %w", key, err)
	}
	return nil
}

func (s *S3Storage) HeadObject(ctx context.Context, key string) (int64, bool, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		es := err.Error()
		if strings.Contains(es, "404") || strings.Contains(es, "NoSuchKey") || strings.Contains(es, "NotFound") {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("head object: %w", err)
	}
	var size int64
	if out.ContentLength != nil {
		size = *out.ContentLength
	}
	return size, true, nil
}
