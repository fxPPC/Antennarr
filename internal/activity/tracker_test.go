package activity

import "testing"

func TestActiveStagesPausedCountsAndBoundedHistory(t *testing.T) {
	tracker := New()
	job := tracker.Begin("account_check", "Checking account #1", 1, 0)
	job.Stage("Refreshing channel list for account #1")
	s := tracker.Snapshot()
	if len(s.Active) != 1 || s.Active[0].Message != "Refreshing channel list for account #1" {
		t.Fatal(s)
	}
	job.Finish("healthy")
	job.Finish("healthy")
	s = tracker.Snapshot()
	if len(s.Active) != 0 || s.Completed["account_check"] != 1 || s.Recent[0].Outcome != "healthy" {
		t.Fatal(s)
	}
	pause := tracker.Begin("stream_check", "Checking CNN", 1, 2)
	pause.Finish("paused")
	if tracker.Snapshot().Completed["stream_check"] != 0 {
		t.Fatal("cancelled probe counted as completed")
	}
	for i := 0; i < 20; i++ {
		tracker.Begin("account_check", "Checking an account", 1, 0).Finish("failed")
	}
	if len(tracker.Snapshot().Recent) != 12 {
		t.Fatal("unbounded history")
	}
}
