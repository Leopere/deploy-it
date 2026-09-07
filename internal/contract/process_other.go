//go:build !darwin && !linux

package contract

import "os/exec"

func configureProcessGroup(cmd *exec.Cmd) {}
