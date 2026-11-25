package backend

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/egoist/mygo/plugins/sqlite"
)

// Restore repository->account identity through the old GitHub repository owner
// table. Never choose an arbitrary account just because its login happens to match.
func (s *NativeStore) migrateAccountLinks(ctx context.Context) error {
	done, err := s.Get(ctx, "native-account-links-v1")
	if err != nil || done == "complete" {
		return err
	}
	rows, err := s.db.Query(ctx, "SELECT table_name,value FROM legacy_records WHERE table_name IN ('S_repositories','S_git^Hub^Repositories','S_owners')")
	if err != nil {
		return err
	}
	type sourceRepo struct {
		ID                 int
		GitHubRepositoryID *int
		Login              *string
	}
	type remoteRepo struct{ ID, OwnerID int }
	type owner struct {
		ID       int
		Endpoint string
	}
	locals := map[int]sourceRepo{}
	remotes := map[int]remoteRepo{}
	owners := map[int]owner{}
	for _, row := range rows.Values {
		data := []byte(unescapeLegacySQLite(row[1].(string)))
		switch row[0].(string) {
		case "S_repositories":
			var r sourceRepo
			if err := json.Unmarshal(data, &r); err != nil {
				return err
			}
			locals[r.ID] = r
		case "S_git^Hub^Repositories":
			var r remoteRepo
			if err := json.Unmarshal(data, &r); err != nil {
				return err
			}
			remotes[r.ID] = r
		case "S_owners":
			var o owner
			if err := json.Unmarshal(data, &o); err != nil {
				return err
			}
			owners[o.ID] = o
		}
	}
	repositories, err := s.Repositories(ctx)
	if err != nil {
		return err
	}
	statements := []sqlite.Statement{}
	for _, r := range repositories {
		old, ok := locals[r.ID]
		if !ok || old.Login == nil || *old.Login == "" || r.Account != nil {
			continue
		}
		if old.GitHubRepositoryID == nil {
			return errors.New("legacy assigned repository lacks its remote account context")
		}
		remote, ok := remotes[*old.GitHubRepositoryID]
		if !ok {
			return errors.New("legacy assigned repository remote record is missing")
		}
		owner, ok := owners[remote.OwnerID]
		if !ok || owner.Endpoint == "" {
			return errors.New("legacy assigned repository endpoint is missing")
		}
		r.Account = &KernelAccountIdentity{Endpoint: owner.Endpoint, Login: *old.Login}
		data, err := json.Marshal(r)
		if err != nil {
			return err
		}
		statements = append(statements, sqlite.Statement{SQL: "UPDATE repositories SET data=? WHERE id=?", Args: []any{string(data), r.ID}})
	}
	statements = append(statements, sqlite.Statement{SQL: "INSERT INTO settings(key,value) VALUES ('native-account-links-v1','complete')"})
	_, err = s.db.Transaction(ctx, statements)
	return err
}
