package catalog

import "time"

// Import jobs describe UI work, not a durable execution queue. After a restart
// there is no worker owning a previously running job; resubmission is safe
// because catalogue ingestion deduplicates entries transactionally.
func (s *Store) RecoverImportJobs() (int64, error) {
	r, e := s.DB.Exec("UPDATE import_jobs SET state='interrupted',finished=?,summary=? WHERE state='running'", time.Now().Unix(), "Import interrupted by restart. Saved entries are retained; resubmit the list to finish.")
	if e != nil {
		return 0, e
	}
	return r.RowsAffected()
}
func (s *Store) StartImportJob() (int64, error) {
	r, e := s.DB.Exec("INSERT INTO import_jobs(started,state) VALUES(?,'running')", time.Now().Unix())
	if e != nil {
		return 0, e
	}
	return r.LastInsertId()
}
func (s *Store) FinishImportJob(id int64, state string, count int, summary string) error {
	_, e := s.DB.Exec("UPDATE import_jobs SET finished=?,state=?,processed=?,summary=? WHERE id=?", time.Now().Unix(), state, count, summary, id)
	return e
}
func (s *Store) PruneImportJobs() error {
	_, e := s.DB.Exec("DELETE FROM import_jobs WHERE state!='running' AND id NOT IN (SELECT id FROM import_jobs ORDER BY id DESC LIMIT 20)")
	return e
}
