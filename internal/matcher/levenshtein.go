package matcher

import (
	"unicode/utf8"
)

// Levenshtein 计算两个字符串的编辑距离
func Levenshtein(a, b string) int {
	ar := []rune(a)
	br := []rune(b)
	la, lb := len(ar), len(br)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	// 使用两行滚动数组节省空间
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

// Similarity 返回相似度（0.0-1.0），1 表示完全相同
func Similarity(a, b string) float64 {
	la, lb := utf8.RuneCountInString(a), utf8.RuneCountInString(b)
	if la == 0 && lb == 0 {
		return 1
	}
	maxLen := la
	if lb > maxLen {
		maxLen = lb
	}
	if maxLen == 0 {
		return 0
	}
	dist := Levenshtein(a, b)
	return 1 - float64(dist)/float64(maxLen)
}

func min3(a, b, c int) int {
	if a < b {
		b = a
	}
	if c < b {
		return c
	}
	return b
}
