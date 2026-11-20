// Package desktopstats defines the counters sent through the child process's
// private output pipe. No admin credentials or source details cross this pipe.
package desktopstats

type Counters struct {
	Viewers   int `json:"viewers"`
	WithFeeds int `json:"with_feeds"`
	Channels  int `json:"channels"`
}
