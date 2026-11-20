package main

import (
	"antennarr/internal/catalog"
	"antennarr/internal/desktopstats"
	"antennarr/internal/stream"
	"context"
	"encoding/json"
	"io"
	"testing"
)

func TestDesktopStatsShowViewersAndEnabledChannelsWithFeeds(t *testing.T) {
	s, err := catalog.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = s.DB.Exec(`
		INSERT INTO accounts(id,identity,provider,kind,secret,created) VALUES(1,'a','example.test','direct','{}',1);
		INSERT INTO streams(id,account_id,identity,name,secret) VALUES(1,1,'s','Feed','{}');
		INSERT INTO channels(id,number,name,enabled) VALUES('a','1','One',1),('b','2','Two',1),
			('c','3','Three',1),('d','4','Four',1),('e','5','Five',1),('f','6','Disabled',0);
		INSERT INTO roster(channel_id,stream_id,account_id,admitted,verified,due)
			SELECT id,1,1,1,1,1 FROM channels WHERE id IN ('a','b','c','f')`)
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer writer.Close()
		writeDesktopStats(ctx, writer, s, func() stream.Status { return stream.Status{Viewers: 2} })
	}()
	var counts desktopstats.Counters
	err = json.NewDecoder(reader).Decode(&counts)
	stop()
	reader.Close()
	<-done
	if err != nil || counts != (desktopstats.Counters{Viewers: 2, WithFeeds: 3, Channels: 5}) {
		t.Fatalf("desktop counters %+v, error %v", counts, err)
	}
}
