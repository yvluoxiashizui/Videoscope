package services

// 全局依赖注入。
// 用接口类型持有，调用方不关心背后是真实服务还是本地模拟实现。
var (
	transcriber  Transcriber
	ocr          OCRExtractor
	llm          LLMClient
	llmModelName string
	embedder     Embedder
)

// SetTranscriber 注入转写器。
func SetTranscriber(t Transcriber) {
	transcriber = t
}

// ASR 返回当前的转写器。
// 未注入时为 nil，调用方需要判空——但那说明 main 的初始化漏了，
// 属于启动期就该发现的问题，所以这里直接返回，不做兜底。
func ASR() Transcriber {
	return transcriber
}

// SetOCRExtractor 注入画面文字识别器。
func SetOCRExtractor(o OCRExtractor) {
	ocr = o
}

// OCR 返回当前的画面文字识别器。
func OCR() OCRExtractor {
	return ocr
}

// SetLLMClient 注入大模型客户端，同时记录模型标识。
//
// 模型名单独存一份是为了让分析结果能追溯到具体的模型与配置：
// 同一条链路换过模型之后，历史结果的差异需要能归因。
func SetLLMClient(c LLMClient, modelName string) {
	llm = c
	llmModelName = modelName
}

// LLM 返回当前的大模型客户端。
func LLM() LLMClient {
	return llm
}

// LLMModelName 返回当前使用的模型标识。
func LLMModelName() string {
	return llmModelName
}

// SetEmbedder 注入向量化服务。
func SetEmbedder(e Embedder) {
	embedder = e
}

// Embed 返回当前的向量化服务。
func Embed() Embedder {
	return embedder
}
