package services

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/lifecycle"
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

// ObjectSize 返回对象字节数。
func ObjectSize(ctx context.Context, key string) (int64, error) {
	info, err := storageClient.StatObject(ctx, storageBucket, key, minio.StatObjectOptions{})
	if err != nil {
		return 0, fmt.Errorf("查询对象 %s: %w", key, err)
	}
	return info.Size, nil
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

// PresignedGetURL 生成一个带签名的临时下载地址。
//
// 桶是私有的，直接用对象路径访问会被拒绝；只有拿到签名地址才能下载。
// 签名里带过期时间，过期后链接自动失效，即使被转发出去也只在一段时间内可用。
// 这样文件不直接暴露在公网，访问控制仍然收敛在后端的鉴权逻辑里。
func PresignedGetURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	u, err := storageClient.PresignedGetObject(ctx, storageBucket, key, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("生成预签名地址: %w", err)
	}
	return u.String(), nil
}

// 存储生命周期规则。
//
// 分片是中间产物，合并成功后就没有价值，留一天足够了；
// 清理动作交给存储层自己执行，不写定时任务——定时任务要额外维护、
// 要处理删除失败的重试，而生命周期规则在存储内部运行，
// 即使应用停止也照常生效。
const (
	ChunkPrefix      = "chunks/"
	ChunkExpireDays  = 1
	ObjectPrefix     = "videos/"
	ObjectExpireDays = 7
)

// EnsureLifecycle 配置桶的生命周期规则，可重复调用。
//
// 原始视频只作为中间素材，处理完之后长期留存没有意义；
// 但业务上还要允许用户回看和追问，所以保留一周而不是立刻删除。
func EnsureLifecycle(ctx context.Context) error {
	cfg := lifecycle.NewConfiguration()
	cfg.Rules = append(cfg.Rules,
		lifecycle.Rule{
			ID:         "expire-chunks",
			Status:     "Enabled",
			RuleFilter: lifecycle.Filter{Prefix: ChunkPrefix},
			Expiration: lifecycle.Expiration{Days: lifecycle.ExpirationDays(ChunkExpireDays)},
		},
		lifecycle.Rule{
			ID:         "expire-objects",
			Status:     "Enabled",
			RuleFilter: lifecycle.Filter{Prefix: ObjectPrefix},
			Expiration: lifecycle.Expiration{Days: lifecycle.ExpirationDays(ObjectExpireDays)},
		},
	)
	if err := storageClient.SetBucketLifecycle(ctx, storageBucket, cfg); err != nil {
		return fmt.Errorf("配置生命周期规则: %w", err)
	}
	return nil
}
