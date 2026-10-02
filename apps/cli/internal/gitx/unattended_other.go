//go:build !linux

package gitx

import "syscall"

// unattendedAttr starts an unattended git in a session of its own, see
// call.unattended. No system but Linux can have git killed when agentx dies,
// so a git whose agentx was killed runs on until it finishes or gives up.
func unattendedAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
