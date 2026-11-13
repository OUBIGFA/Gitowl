package main

import (
	"net/url"
	"strings"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
)

type nativeProjectRow struct {
	repository *backend.KernelRepository
	account    *backend.KernelAccountIdentity
}

// Group only by Desktop's explicit account identity. Unassigned repositories
// and in-progress clones stay separate; an organization name never picks an
// account. Filtering keeps the original rank within each group.
func projectRows(repositories []backend.KernelRepository, accounts []backend.KernelAccount) []nativeProjectRow {
	type group struct {
		account *backend.KernelAccountIdentity
		rows    []nativeProjectRow
	}
	var groups []group
	var unassigned []nativeProjectRow
	byAccount := make(map[string]int)
	for i := range repositories {
		repository := &repositories[i]
		row := nativeProjectRow{repository: repository}
		if repository.Account == nil {
			unassigned = append(unassigned, row)
			continue
		}
		key := repository.Account.Endpoint + "\x00" + strings.ToLower(repository.Account.Login)
		index, found := byAccount[key]
		if !found {
			index = len(groups)
			byAccount[key] = index
			groups = append(groups, group{account: repository.Account})
		}
		groups[index].rows = append(groups[index].rows, row)
	}
	if len(accounts) < 2 && len(groups) < 2 {
		rows := make([]nativeProjectRow, len(repositories))
		for i := range repositories {
			rows[i].repository = &repositories[i]
		}
		return rows
	}
	rows := make([]nativeProjectRow, 0, len(repositories)+len(groups)+1)
	for _, group := range groups {
		rows = append(rows, nativeProjectRow{account: group.account})
		rows = append(rows, group.rows...)
	}
	if len(unassigned) > 0 {
		rows = append(rows, nativeProjectRow{})
		rows = append(rows, unassigned...)
	}
	return rows
}

func projectGroupLabel(locale string, account *backend.KernelAccountIdentity) string {
	if account == nil {
		return workspaceLabel(locale, "workspace.unassignedProjects")
	}
	host := account.Endpoint
	if endpoint, err := url.Parse(account.Endpoint); err == nil && endpoint.Host != "" {
		host = endpoint.Host
	}
	if host == "api.github.com" {
		host = "github.com"
	}
	return "@" + account.Login + " · " + host
}
