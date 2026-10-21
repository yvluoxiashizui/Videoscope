package services

// transcriber 全局转写器，由 main 注入。
// 用接口类型持有，调用方不关心背后是真实服务还是模拟实现。
var transcriber Transcriber

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
