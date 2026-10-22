package services

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"videoscope/models"
	"videoscope/repos"
)

// TopK 是默认召回数量。
//
// 三到五块通常能覆盖一个问题的证据范围。取得太少容易漏掉关键信息，
// 取得太多会把无关内容一起塞给模型，反而稀释重点、抬高成本。
const TopK = 4

// Hit 是一条检索命中。
type Hit struct {
	Chunk Chunk
	Score float64
}

// IndexTask 为某个任务建立检索索引：切块、向量化、落库。
//
// 索引在视频处理完成时建立。放在这一步而不是查询时临时计算，
// 是因为向量化本身要调外部接口，有延迟也有成本，
// 放在查询路径上会让每个提问都多等一次网络往返。
func IndexTask(ctx context.Context, taskID uint, vctx *VideoContext) (int, error) {
	chunks := ChunkContext(vctx)
	if len(chunks) == 0 {
		return 0, nil
	}

	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}

	vectors, err := Embed().Embed(ctx, texts)
	if err != nil {
		return 0, fmt.Errorf("生成检索块向量: %w", err)
	}
	if len(vectors) != len(chunks) {
		return 0, fmt.Errorf("向量数量与块数量不一致：%d vs %d", len(vectors), len(chunks))
	}

	records := make([]models.VideoChunk, len(chunks))
	for i, c := range chunks {
		encoded, err := json.Marshal(vectors[i])
		if err != nil {
			return 0, fmt.Errorf("序列化向量: %w", err)
		}
		records[i] = models.VideoChunk{
			TaskID:       taskID,
			ChunkIndex:   c.Index,
			StartSeconds: c.Start,
			EndSeconds:   c.End,
			Content:      c.Content,
			Embedding:    string(encoded),
		}
	}

	if err := repos.SaveChunks(taskID, records); err != nil {
		return 0, fmt.Errorf("保存检索块: %w", err)
	}
	return len(records), nil
}

// Retrieve 根据问题召回最相关的若干块。
//
// 先把问题向量化，再和每一块的向量算余弦相似度，取分数最高的若干个。
// 当前是线性扫描全部块——单任务几十块，一次比较的开销可以忽略，
// 换成向量数据库的收益要等数据量再大一个数量级才体现出来。
func Retrieve(ctx context.Context, taskID uint, question string, topK int) ([]Hit, error) {
	if topK <= 0 {
		topK = TopK
	}

	records, err := repos.FindChunksByTask(taskID)
	if err != nil {
		return nil, fmt.Errorf("读取检索块: %w", err)
	}
	if len(records) == 0 {
		return nil, nil
	}

	queryVecs, err := Embed().Embed(ctx, []string{question})
	if err != nil {
		return nil, fmt.Errorf("向量化问题: %w", err)
	}
	if len(queryVecs) == 0 {
		return nil, fmt.Errorf("问题向量化结果为空")
	}
	query := queryVecs[0]

	hits := make([]Hit, 0, len(records))
	for _, r := range records {
		var vec []float32
		if err := json.Unmarshal([]byte(r.Embedding), &vec); err != nil {
			// 单条向量损坏不影响其他块，跳过。
			continue
		}
		hits = append(hits, Hit{
			Chunk: Chunk{
				Index:   r.ChunkIndex,
				Start:   r.StartSeconds,
				End:     r.EndSeconds,
				Content: r.Content,
			},
			Score: cosineSimilarity(query, vec),
		})
	}

	// 按相似度降序，取前 topK。
	sort.Slice(hits, func(i, j int) bool {
		return hits[i].Score > hits[j].Score
	})
	if len(hits) > topK {
		hits = hits[:topK]
	}
	return hits, nil
}

// cosineSimilarity 计算余弦相似度。
//
// 向量在生成时已经归一化，理论上直接点积即可；
// 这里仍然完整计算，是因为换过 embedding 服务之后
// 归一化与否不由我方控制，算全了更稳。
func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}

	var dot, normA, normB float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		normA += x * x
		normB += y * y
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
