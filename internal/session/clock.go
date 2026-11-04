package session

import "time"

// Keepalive timestamps are kept in atomics as nanoseconds on the monotonic
// clock, counted from process start. time.Unix would read them back as wall
// clock times, and a clock step (NTP, a VM resuming, a manual change) would
// then make a healthy connection look dead or a dead one look alive.
var clockBase = time.Now()

// MonoNow returns the current time for storing in an atomic. It is never 0,
// so 0 can mean "not set".
func MonoNow() int64 { return int64(time.Since(clockBase)) + 1 }

// MonoTime turns a MonoNow value back into a time.Time that compares with
// time.Now() on the monotonic clock.
func MonoTime(t int64) time.Time { return clockBase.Add(time.Duration(t - 1)) }
