package missives

import (
	"testing"
	"time"
)

func TestStartMissiveScheduleCoalescesConcurrentRuns(t *testing.T) {
	const uid = uint64(900001)
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})

	if ok := startMissiveSchedule(uid, func() {
		close(started)
		<-release
		close(done)
	}); !ok {
		t.Fatal("first schedule did not start")
	}
	<-started

	if ok := startMissiveSchedule(uid, func() {}); ok {
		t.Fatal("concurrent schedule was not coalesced")
	}

	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("schedule did not finish")
	}

	deadline := time.Now().Add(time.Second)
	for {
		if ok := startMissiveSchedule(uid, func() {}); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("completed schedule remained active")
		}
		time.Sleep(time.Millisecond)
	}
}
