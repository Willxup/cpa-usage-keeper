//go:build linux

package quota

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func suspendAwareTimerParameters(delay time.Duration) (int, int, int, unix.ItimerSpec) {
	// 使用相对的一次性 CLOCK_BOOTTIME timer，让系统休眠时间计入自动刷新等待周期。
	return unix.CLOCK_BOOTTIME,
		unix.TFD_CLOEXEC | unix.TFD_NONBLOCK,
		0,
		unix.ItimerSpec{Value: unix.NsecToTimespec(delay.Nanoseconds())}
}

func newSuspendAwareTimer(delay time.Duration) (<-chan time.Time, func(), error) {
	if delay <= 0 {
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch, func() {}, nil
	}

	clockID, createFlags, settimeFlags, spec := suspendAwareTimerParameters(delay)
	fd, err := unix.TimerfdCreate(clockID, createFlags)
	if err != nil {
		return nil, nil, err
	}

	if err := unix.TimerfdSettime(fd, settimeFlags, &spec, nil); err != nil {
		_ = unix.Close(fd)
		return nil, nil, err
	}

	file := os.NewFile(uintptr(fd), "suspend-aware-timer")
	ch := make(chan time.Time, 1)
	go func() {
		var buf [8]byte
		n, err := file.Read(buf[:])
		if err == nil && n == 8 {
			select {
			case ch <- time.Now():
			default:
			}
		}
	}()

	stop := func() {
		_ = file.Close()
	}

	return ch, stop, nil
}
