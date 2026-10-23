//go:build !darwin && !linux

package contract

import (
	"errors"
	"os"
	"os/exec"
)

func detachWorker(cmd *exec.Cmd) {}
func lockFile(path string, nonblocking bool) (*os.File, error) {
	return nil, errors.New("background deployments require Linux or macOS process locks")
}
func unlockFile(file *os.File)  {}
func isLockBusy(err error) bool { return false }
