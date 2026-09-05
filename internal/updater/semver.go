// Package updater 自动更新（M14）：版本比较 / 镜像测速 / 检查更新 / 一键安装。
package updater

import (
	"fmt"
	"strconv"
	"strings"
)

// SemVer 语义化版本（v主.次.修订；v0.0.99 → v0.1.0 正确进位）
type SemVer struct {
	Major int
	Minor int
	Patch int
}

// Parse 解析版本号，带 v 前缀或纯数字均可
func Parse(s string) (SemVer, error) {
	t := s
	if i := strings.IndexByte(t, 'v'); i == 0 {
		t = t[1:]
	}
	// 去掉可能的后缀（如 -beta / +build）
	if i := strings.IndexAny(t, "-+"); i >= 0 {
		t = t[:i]
	}
	parts := strings.Split(t, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return SemVer{}, fmt.Errorf("非法版本号: %s", s)
	}
	num := func(i int) int {
		if i >= len(parts) || parts[i] == "" {
			return 0
		}
		n, _ := strconv.Atoi(parts[i])
		return n
	}
	return SemVer{Major: num(0), Minor: num(1), Patch: num(2)}, nil
}

// String 输出 v大.次.修订
func (v SemVer) String() string {
	return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Compare 比较：a 与 b，返回 -1/0/1（a<b → -1）
func (a SemVer) Compare(b SemVer) int {
	cmp := cmp3(a.Major, b.Major)
	if cmp != 0 {
		return cmp
	}
	cmp = cmp3(a.Minor, b.Minor)
	if cmp != 0 {
		return cmp
	}
	return cmp3(a.Patch, b.Patch)
}

// GreaterThan 判断 a 是否大于 b
func (a SemVer) GreaterThan(b SemVer) bool { return a.Compare(b) > 0 }

func cmp3(x, y int) int {
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}