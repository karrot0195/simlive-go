package storage

import (
	"bytes"
	"fmt"
	"os/exec"
)

type RcloneService struct{}

func NewRcloneService() *RcloneService {
	return &RcloneService{}
}

// CatReadsExec - Gọi `rclone cat` đọc manifest M3U8 từ S3/MinIO
func (r *RcloneService) Cat(remotePath string) (string, error) {
	cmd := exec.Command("rclone", "cat", remotePath)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("rclone cat error: %v, stderr: %s", err, stderr.String())
	}
	return out.String(), nil
}

// CopyTo - Tải `.ts` file về thư mục đệm cục bộ
func (r *RcloneService) CopyTo(srcRemotePath, localDestPath string) error {
	cmd := exec.Command("rclone", "copyto", srcRemotePath, localDestPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("rclone copyto error: %v, stderr: %s", err, stderr.String())
	}
	return nil
}
