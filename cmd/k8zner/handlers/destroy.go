package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/milankappen/k8zner/internal/config"
	"github.com/milankappen/k8zner/internal/platform/cloudflare"
	"github.com/milankappen/k8zner/internal/platform/dns"
	"github.com/milankappen/k8zner/internal/provisioning/destroy"
)

const (
	// s3MetadataFile is the name of the metadata file used to verify bucket ownership.
	s3MetadataFile = "k8zner_metadata.json"
)

// Destroy handles the destroy command.
//
// It loads the cluster configuration and deletes all associated resources
// from Hetzner Cloud. Resources are deleted in dependency order.
// Also cleans up S3 buckets matching the cluster naming convention.
func Destroy(ctx context.Context, configPath string) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	log.Printf("Destroying cluster: %s", cfg.ClusterName)

	token := os.Getenv("HCLOUD_TOKEN")
	infraClient := newInfraClient(token)

	pCtx := newProvisioningContext(ctx, cfg, infraClient, nil)

	if err := destroy.Destroy(pCtx); err != nil {
		return fmt.Errorf("destroy failed: %w", err)
	}

	// Clean up Cloudflare DNS records owned by this cluster
	if cfg.Addons.Cloudflare.Enabled && cfg.Addons.Cloudflare.APIToken != "" && cfg.Addons.Cloudflare.Domain != "" {
		log.Println("Cleaning up Cloudflare DNS records...")
		if err := cleanupCloudflareDNS(ctx, cfg); err != nil {
			log.Printf("Warning: Cloudflare DNS cleanup failed: %v", err)
		}
	}

	// Clean up S3 buckets if talos-backup was configured
	if cfg.Addons.TalosBackup.Enabled {
		log.Println("Cleaning up S3 buckets...")
		if err := cleanupS3Buckets(ctx, cfg.ClusterName, cfg.Addons.TalosBackup); err != nil {
			log.Printf("Warning: S3 cleanup failed: %v", err)
		}
	}

	log.Printf("Cluster %s destroyed successfully", cfg.ClusterName)
	return nil
}

// cleanupS3Buckets removes S3 buckets matching the cluster naming convention.
func cleanupS3Buckets(ctx context.Context, clusterName string, backupCfg config.TalosBackupConfig) error {
	endpoint := backupCfg.S3Endpoint
	accessKey := backupCfg.S3AccessKey
	secretKey := backupCfg.S3SecretKey
	region := backupCfg.S3Region

	if endpoint == "" || accessKey == "" || secretKey == "" {
		log.Println("S3 credentials not configured, skipping bucket cleanup")
		return nil
	}

	if region == "" {
		region = "us-east-1"
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		return fmt.Errorf("failed to create S3 config: %w", err)
	}

	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})

	result, err := s3Client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return fmt.Errorf("failed to list S3 buckets: %w", err)
	}

	prefix := clusterName + "-"
	for _, bucket := range result.Buckets {
		if bucket.Name == nil || !strings.HasPrefix(*bucket.Name, prefix) {
			continue
		}

		bucketName := *bucket.Name

		owned, err := verifyBucketOwnership(ctx, s3Client, bucketName, clusterName)
		if err != nil {
			log.Printf("Warning: failed to verify ownership of bucket %s: %v", bucketName, err)
			continue
		}

		if !owned {
			log.Printf("Skipping bucket %s: ownership not verified (no valid metadata)", bucketName)
			continue
		}

		log.Printf("Deleting S3 bucket: %s", bucketName)
		if err := deleteS3Bucket(ctx, s3Client, bucketName); err != nil {
			log.Printf("Warning: failed to delete bucket %s: %v", bucketName, err)
		}
	}

	return nil
}

// s3BucketMetadata represents the metadata stored in k8zner-managed buckets.
type s3BucketMetadata struct {
	ClusterName string `json:"clusterName"`
	ManagedBy   string `json:"managedBy"`
	CreatedAt   string `json:"createdAt,omitempty"`
}

// verifyBucketOwnership checks if a bucket is owned by the specified cluster.
func verifyBucketOwnership(ctx context.Context, s3Client *s3.Client, bucketName, expectedCluster string) (bool, error) {
	result, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(s3MetadataFile),
	})
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "NoSuchKey") || strings.Contains(errStr, "not found") || strings.Contains(errStr, "404") {
			knownPatterns := []string{
				expectedCluster + "-etcd-backup",
				expectedCluster + "-talos-backup",
			}
			for _, pattern := range knownPatterns {
				if bucketName == pattern {
					log.Printf("Bucket %s has no metadata file but matches known k8zner pattern, allowing deletion", bucketName)
					return true, nil
				}
			}
			return false, nil
		}
		return false, fmt.Errorf("failed to get metadata: %w", err)
	}
	defer func() { _ = result.Body.Close() }()

	data, err := io.ReadAll(result.Body)
	if err != nil {
		return false, fmt.Errorf("failed to read metadata: %w", err)
	}

	var metadata s3BucketMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return false, fmt.Errorf("failed to parse metadata: %w", err)
	}

	if metadata.ClusterName != expectedCluster {
		log.Printf("Bucket %s belongs to cluster %s, not %s", bucketName, metadata.ClusterName, expectedCluster)
		return false, nil
	}

	if metadata.ManagedBy != "k8zner" {
		log.Printf("Bucket %s is not managed by k8zner (managedBy: %s)", bucketName, metadata.ManagedBy)
		return false, nil
	}

	return true, nil
}

// deleteS3Bucket empties and deletes an S3 bucket.
func deleteS3Bucket(ctx context.Context, client *s3.Client, bucketName string) error {
	listInput := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
	}

	paginator := s3.NewListObjectsV2Paginator(client, listInput)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list objects: %w", err)
		}

		for _, obj := range page.Contents {
			_, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(bucketName),
				Key:    obj.Key,
			})
			if err != nil {
				log.Printf("Warning: failed to delete object %s: %v", *obj.Key, err)
			}
		}
	}

	_, err := client.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		return fmt.Errorf("failed to delete bucket: %w", err)
	}

	return nil
}

// newDNSProvider creates the DNS backend used for record cleanup.
// Package-level for test injection, like the other handler factories.
var newDNSProvider = func(apiToken string) dns.Provider {
	return cloudflare.NewClient(apiToken)
}

// cleanupCloudflareDNS removes DNS records owned by this cluster from Cloudflare.
// Records are identified via TXT ownership records created by external-dns.
func cleanupCloudflareDNS(ctx context.Context, cfg *config.Config) error {
	provider := newDNSProvider(cfg.Addons.Cloudflare.APIToken)

	zoneID := cfg.Addons.Cloudflare.ZoneID
	if zoneID == "" {
		var err error
		zoneID, err = provider.GetZoneID(ctx, cfg.Addons.Cloudflare.Domain)
		if err != nil {
			return fmt.Errorf("failed to get zone ID for %s: %w", cfg.Addons.Cloudflare.Domain, err)
		}
	}

	// Use TXT owner ID if configured, otherwise default to cluster name
	ownerID := cfg.Addons.ExternalDNS.TXTOwnerID
	if ownerID == "" {
		ownerID = cfg.ClusterName
	}

	count, err := provider.CleanupClusterRecords(ctx, zoneID, ownerID)
	if err != nil {
		return fmt.Errorf("failed to clean up DNS records: %w", err)
	}

	if count > 0 {
		log.Printf("Deleted %d Cloudflare DNS records owned by cluster %s", count, ownerID)
	} else {
		log.Println("No Cloudflare DNS records found for this cluster")
	}

	return nil
}
