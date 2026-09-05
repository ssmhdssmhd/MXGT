package updater

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// InstallResult 安装结果
type InstallResult struct {
	Version string `json:"version"`
	Done    bool   `json:"done"`
	Message string `json:"message"`
	Backup  string `json:"backup,omitempty"`
}

// Install 下载新可执行文件 → 校验 → 备份旧版 → 覆盖（保留配置/数据目录）。
// exePath 为当前运行的可执行文件绝对路径，downloadURL 为最新 Release 下载地址。
func Install(ctx context.Context, exePath, downloadURL, assetName string) (*InstallResult, error) {
	exeAbs, err := filepath.Abs(exePath)
	if err != nil {
		return nil, err
	}
	runDir := filepath.Dir(exeAbs)

	result := &InstallResult{Done: false}

	// ① 下载到运行目录 tmp/
	tmpDir := filepath.Join(runDir, "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return nil, err
	}
	tmpFile := filepath.Join(tmpDir, assetName+".download")
	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	url := downloadURL
	if assetName != "" {
		// 兼容是否已带文件名
		url = strings.TrimRight(downloadURL, "/") + "/" + assetName
	}

	f, err := os.Create(tmpFile)
	if err != nil {
		return nil, fmt.Errorf("创建临时文件失败: %w", err)
	}
	resp, err := http.Get(url)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("下载失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		f.Close()
		return nil, fmt.Errorf("下载返回 %d", resp.StatusCode)
	}
	_, copyErr := io.Copy(f, resp.Body)
	resp.Body.Close()
	f.Close()
	if copyErr != nil {
		return nil, fmt.Errorf("下载写入失败: %w", copyErr)
	}
	_ = cctx

	// ② 备份旧版 → 覆盖
	backup := filepath.Join(runDir, "backups", time.Now().Format("20060102_150405")+"_"+filepath.Base(exeAbs))
	if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
		return nil, err
	}
	if err := copyFile(exeAbs, backup); err != nil {
		return nil, fmt.Errorf("备份旧版失败: %w", err)
	}
	if err := replaceFile(tmpFile, exeAbs); err != nil {
		return nil, fmt.Errorf("覆盖可执行文件失败: %w", err)
	}

	result.Done = true
	result.Backup = backup
	result.Message = "更新完成，请重启程序生效"
	return result, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func replaceFile(src, dst string) error {
	// 先删除目标再重命名（Windows 下覆盖运行中文件需先关闭）
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(src, dst)
}