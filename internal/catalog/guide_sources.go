package catalog

import "context"

// GuideRoster contains only verified roster associations. Account secrets are
// opened separately for the account actually selected for an XMLTV request.
type GuideRoster struct {
	ChannelID, Station, Provider, Kind string
	Account                            int64
	Limit                              int
}

func (s *Store) GuideRosters(ctx context.Context) ([]GuideRoster, error) {
	rows, e := s.DB.QueryContext(ctx, `SELECT DISTINCT r.channel_id,s.tvg_id,a.provider,a.kind,a.id,max(0,a.connection_limit-a.external_connections)
 FROM roster r JOIN streams s ON s.id=r.stream_id JOIN accounts a ON a.id=r.account_id
 JOIN active_channels c ON c.id=r.channel_id
 WHERE c.enabled=1 AND s.enabled=1 AND a.state NOT IN ('inactive','retired','manually_blocked')
 ORDER BY a.id,r.channel_id,s.tvg_id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var list []GuideRoster
	for rows.Next() {
		var g GuideRoster
		if e = rows.Scan(&g.ChannelID, &g.Station, &g.Provider, &g.Kind, &g.Account, &g.Limit); e != nil {
			return nil, e
		}
		list = append(list, g)
	}
	return list, rows.Err()
}

// GuideAllowed applies the same explicit market, station and timeshift
// exclusions used when choosing streams. An opaque upstream ID alone adds no
// regional evidence; the verified roster association supplies the nomination.
func GuideAllowed(ch Channel, id, name string) bool {
	m := compileChannel(ch)
	label := sourceLabelWithID(name, "", id)
	country := regionCountry(ch.Region)
	if country != "" && ((label.guide.country != "" && country != label.guide.country) || (label.country != "" && country != label.country)) {
		return false
	}
	return !m.excludes(label) && m.stationMatches(label)
}
