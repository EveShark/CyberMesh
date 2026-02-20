package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type S3Writer struct {
	cfg      Config
	client   *s3.Client
	uploader *manager.Uploader
}

func NewS3Writer(cfg Config) (*S3Writer, error) {
	if strings.TrimSpace(cfg.Region) == "" {
		cfg.Region = "us-east-1"
	}

	loadOptions := []func(*config.LoadOptions) error{
		config.WithRegion(cfg.Region),
	}

	if strings.TrimSpace(cfg.AccessKey) != "" && strings.TrimSpace(cfg.SecretKey) != "" {
		creds := credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, cfg.SessionToken)
		loadOptions = append(loadOptions, config.WithCredentialsProvider(creds))
	}

	if strings.TrimSpace(cfg.Endpoint) != "" {
		resolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, _ ...interface{}) (aws.Endpoint, error) {
			if service == s3.ServiceID {
				return aws.Endpoint{
					URL:               cfg.Endpoint,
					SigningRegion:     cfg.Region,
					HostnameImmutable: true,
				}, nil
			}
			return aws.Endpoint{}, &aws.EndpointNotFoundError{}
		})
		loadOptions = append(loadOptions, config.WithEndpointResolverWithOptions(resolver))
	}

	awsCfg, err := config.LoadDefaultConfig(context.Background(), loadOptions...)
	if err != nil {
		return nil, err
	}

	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.UsePathStyle = cfg.ForcePathStyle
	})
	uploader := manager.NewUploader(client)
	return &S3Writer{cfg: cfg, client: client, uploader: uploader}, nil
}

func (s *S3Writer) Put(ctx context.Context, key string, body io.Reader, size int64, contentType string, retentionUntil int64) (Result, error) {
	if body == nil {
		return Result{}, errors.New("body required")
	}
	if strings.TrimSpace(key) == "" {
		return Result{}, errors.New("key required")
	}
	if contentType == "" {
		contentType = s.cfg.ContentType
	}
	if contentType == "" {
		contentType = "application/vnd.tcpdump.pcap"
	}

	hasher := sha256.New()
	counter := &countingReader{reader: body, hasher: hasher}

	uploadCtx := ctx
	if s.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		uploadCtx, cancel = context.WithTimeout(ctx, s.cfg.Timeout)
		defer cancel()
	}

	input := &s3.PutObjectInput{
		Bucket:      aws.String(s.cfg.Bucket),
		Key:         aws.String(key),
		Body:        counter,
		ContentType: aws.String(contentType),
	}
	if size > 0 {
		input.ContentLength = aws.Int64(size)
	}
	if strings.TrimSpace(s.cfg.ServerSideEnc) != "" {
		input.ServerSideEncryption = s3types.ServerSideEncryption(s.cfg.ServerSideEnc)
	}
	if strings.TrimSpace(s.cfg.KMSKeyID) != "" {
		input.SSEKMSKeyId = aws.String(s.cfg.KMSKeyID)
	}
	if len(s.cfg.Tagging) > 0 {
		input.Tagging = aws.String(encodeTags(s.cfg.Tagging))
	}

	_, err := s.uploader.Upload(uploadCtx, input)
	if err != nil {
		return Result{}, err
	}

	return Result{
		URI:            fmt.Sprintf("s3://%s/%s", s.cfg.Bucket, key),
		ChecksumSHA256: hex.EncodeToString(hasher.Sum(nil)),
		BytesWritten:   counter.count,
		RetentionUntil: retentionUntil,
		ContentType:    contentType,
	}, nil
}

func encodeTags(tags map[string]string) string {
	var parts []string
	for key, value := range tags {
		if strings.TrimSpace(key) == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%s", escapeTag(key), escapeTag(value)))
	}
	return strings.Join(parts, "&")
}

func escapeTag(value string) string {
	replacer := strings.NewReplacer(" ", "+", "&", "%26", "=", "%3D")
	return replacer.Replace(value)
}

type countingReader struct {
	reader io.Reader
	hasher hashWriter
	count  int64
}

type hashWriter interface {
	Write(p []byte) (int, error)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.reader.Read(p)
	if n > 0 {
		c.count += int64(n)
		_, _ = c.hasher.Write(p[:n])
	}
	return n, err
}
