//go:build linux

package gitx

import "syscall"

// unattendedAttr starts an unattended git in a session of its own, see
// call.unattended, and has the kernel kill it when agentx dies. A session of
// its own is out of reach of the signal that kills agentx's process group or
// tree, so without that git would outlive a killed agentx, with no budget
// left to end it. The kernel sends the signal when the thread that started
// git ends, not the process; Go ends a thread only under a goroutine that
// locked itself to it and exits, which no git call runs on. Only git gets
// it: its transport, ssh or a remote helper, ends once it finds git gone,
// or gives up on its own.
func unattendedAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true, Pdeathsig: syscall.SIGKILL}
}
