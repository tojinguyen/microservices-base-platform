package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	uploadConfig "github.com/tojinguyen/upload/internal/config"
)

type S3Storage struct {
	client    *s3.Client
	presigner *s3.PresignClient
	bucket    string
	publicURL string
}

func NewS3Storage(cfg uploadConfig.StorageConfig) (*S3Storage, error) {
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		),
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	clientOpts := []func(*s3.Options){
		func(o *s3.Options) {
			o.UsePathStyle = cfg.UsePathStyle
		},
	}
	if cfg.Endpoint != "" {
		clientOpts = append(clientOpts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		})
	}

	client := s3.NewFromConfig(awsCfg, clientOpts...)
	presigner := s3.NewPresignClient(client)

	publicURL := cfg.PublicURL
	if publicURL == "" {
		if cfg.Endpoint != "" {
			publicURL = fmt.Sprintf("%s/%s", strings.TrimRight(cfg.Endpoint, "/"), cfg.Bucket)
		} else {
			publicURL = fmt.Sprintf("https://%s.s3.%s.amazonaws.com", cfg.Bucket, cfg.Region)
		}
	}

	return &S3Storage{
		client:    client,
		presigner: presigner,
		bucket:    cfg.Bucket,
		publicURL: publicURL,
	}, nil
}

func (s *S3Storage) PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (string, time.Time, error) {
	req, err := s.presigner.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to presign put: %w", err)
	}
	expiresAt := time.Now().Add(ttl)
	return req.URL, expiresAt, nil
}

func (s *S3Storage) HeadObject(ctx context.Context, key string) (int64, string, bool, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "404") || strings.Contains(errStr, "NoSuchKey") || strings.Contains(errStr, "NotFound") {
			return 0, "", false, nil
		}
		return 0, "", false, fmt.Errorf("head object failed: %w", err)
	}

	var size int64
	if out.ContentLength != nil {
		size = *out.ContentLength
	}
	etag := ""
	if out.ETag != nil {
		etag = strings.Trim(*out.ETag, `"`)
	}
	return size, etag, true, nil
}

func (s *S3Storage) DeleteObject(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("delete object failed: %w", err)
	}
	return nil
}

func (s *S3Storage) PublicURL(key string) string {
	return fmt.Sprintf("%s/%s", s.publicURL, key)
}

func (s *S3Storage) CreateMultipartUpload(ctx context.Context, objectKey, mimeType string) (string, error) {
	out, err := s.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(objectKey),
		ContentType: aws.String(mimeType),
	})
	if err != nil {
		return "", fmt.Errorf("create multipart upload failed: %w", err)
	}
	return aws.ToString(out.UploadId), nil
}

func (s *S3Storage) GeneratePresignedPartURL(ctx context.Context, objectKey, uploadID string, partNumber int32, ttl time.Duration) (string, time.Time, error) {
	req, err := s.presigner.PresignUploadPart(ctx, &s3.UploadPartInput{
		Bucket:     aws.String(s.bucket),
		Key:        aws.String(objectKey),
		UploadId:   aws.String(uploadID),
		PartNumber: aws.Int32(partNumber),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("presign part upload failed: %w", err)
	}
	return req.URL, time.Now().Add(ttl), nil
}

func (s *S3Storage) CompleteMultipartUpload(ctx context.Context, objectKey, uploadID string, parts []CompletedPart) (string, error) {
	completed := make([]types.CompletedPart, len(parts))
	for i, p := range parts {
		etag := p.ETag
		if !strings.HasPrefix(etag, `"`) {
			etag = `"` + etag + `"`
		}
		completed[i] = types.CompletedPart{
			PartNumber: aws.Int32(p.PartNumber),
			ETag:       aws.String(etag),
		}
	}

	out, err := s.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(s.bucket),
		Key:      aws.String(objectKey),
		UploadId: aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completed,
		},
	})
	if err != nil {
		return "", fmt.Errorf("complete multipart upload failed: %w", err)
	}
	etag := strings.Trim(aws.ToString(out.ETag), `"`)
	return etag, nil
}

func (s *S3Storage) AbortMultipartUpload(ctx context.Context, objectKey, uploadID string) error {
	_, err := s.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(s.bucket),
		Key:      aws.String(objectKey),
		UploadId: aws.String(uploadID),
	})
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "404") || strings.Contains(errStr, "NoSuchUpload") || strings.Contains(errStr, "NotFound") {
			return nil
		}
		return fmt.Errorf("abort multipart upload failed: %w", err)
	}
	return nil
}

func (s *S3Storage) ListMultipartParts(ctx context.Context, objectKey, uploadID string) ([]PartInfo, error) {
	var result []PartInfo
	var partNumberMarker *string

	for {
		input := &s3.ListPartsInput{
			Bucket:           aws.String(s.bucket),
			Key:              aws.String(objectKey),
			UploadId:         aws.String(uploadID),
			PartNumberMarker: partNumberMarker,
		}

		out, err := s.client.ListParts(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("list parts failed: %w", err)
		}

		for _, p := range out.Parts {
			result = append(result, PartInfo{
				PartNumber:   aws.ToInt32(p.PartNumber),
				ETag:         strings.Trim(aws.ToString(p.ETag), `"`),
				SizeBytes:    aws.ToInt64(p.Size),
				LastModified: aws.ToTime(p.LastModified),
			})
		}

		if !aws.ToBool(out.IsTruncated) {
			break
		}
		partNumberMarker = out.NextPartNumberMarker
	}

	return result, nil
}
