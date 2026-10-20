package services

import (
	"context"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// 对象存储客户端。
// 分片与成品都放在同一个桶里，用 key 前缀区分：
// 分片是 chunks/{md5}/{序号}，成品是 videos/{任务ID}。
var (
	storageClient *minio.Client
	storageBucket string
)

// InitStorage 初始化对象存储客户端并确保桶存在。
func InitStorage(endpoint, accessKey, secretKey, bucket string, useSSL bool) error {
	cli, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return fmt.Errorf("初始化对象存储客户端: %w", err)
	}

	ctx := context.Background()
	exists, err := cli.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("检查桶是否存在: %w", err)
	}
	if !exists {
		if err := cli.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("创建桶 %s: %w", bucket, err)
		}
	}

	storageClient = cli
	storageBucket = bucket
	return nil
}

// ChunkKey 返回分片在桶内的路径。
// 序号补零到六位，保证按字典序排列时分片顺序正确——
// 合并请求依赖这个顺序，不补零的话第 10 片会排在第 2 片前面。
func ChunkKey(md5 string, index int) string {
	return fmt.Sprintf("chunks/%s/%06d", md5, index)
}

// PutChunk 写入一个分片。
func PutChunk(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := storageClient.PutObject(ctx, storageBucket, key, r, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	return err
}

// ObjectExists 判断对象是否已存在，用于秒传判断与合并前的完整性检查。
func ObjectExists(ctx context.Context, key string) (bool, error) {
	_, err := storageClient.StatObject(ctx, storageBucket, key, minio.StatObjectOptions{})
	if err != nil {
		resp := minio.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ComposeChunks 把已上传的分片按序号拼接成一个完整对象。
//
// 对象存储的合并是元数据层面的拼接，不做内容拷贝，所以很快。
// 但它有两条硬性约束：除最后一片外每片不能小于 5MB，总分片数不超过一万。
// 前端切片大小必须满足第一条，否则这里会直接返回错误。
func ComposeChunks(ctx context.Context, md5 string, total int, destKey string) error {
	srcs := make([]minio.CopySrcOptions, 0, total)
	for i := 0; i < total; i++ {
		srcs = append(srcs, minio.CopySrcOptions{
			Bucket: storageBucket,
			Object: ChunkKey(md5, i),
		})
	}

	_, err := storageClient.ComposeObject(ctx, minio.CopyDestOptions{
		Bucket: storageBucket,
		Object: destKey,
	}, srcs...)
	if err != nil {
		return fmt.Errorf("合并分片: %w", err)
	}
	return nil
}
