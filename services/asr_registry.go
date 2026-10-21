package services

// 全局依赖注入。
// 用接口类型持有，调用方不关心背后是真实服务还是本地模拟实现。
var (
	transcriber Transcriber
	ocr         OCRExtractor
	llm         LLMClient
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

// SetLLMClient 注入大模型客户端。
func SetLLMClient(c LLMClient) {
	llm = c
}

// LLM 返回当前的大模型客户端。
func LLM() LLMClient {
	return llm
}
