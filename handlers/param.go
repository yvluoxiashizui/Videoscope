package handlers

import (
	"strconv"
	"strings"
)

// parseUint 解析无符号整数参数。
// 抽成独立函数是为了强制调用方处理错误——
// 如果直接写 strconv.ParseUint 并忽略第二个返回值，
// 非法输入会静默变成 0 继续往下走，最终表现为「查不到数据」而不是「参数错误」。
func parseUint(s string) (uint, error) {
	n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, err
	}
	return uint(n), nil
}
