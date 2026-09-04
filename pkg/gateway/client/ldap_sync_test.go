package client

import "testing"

func TestNormalizeLDAPSyncUsers(t *testing.T) {
	valid, summary := normalizeLDAPSyncUsers([]LDAPSyncUser{
		{ID: "  stable-1 ", Email: " USER@example.com ", Username: "user"},
		{ID: "stable-1", Email: "other@example.com"},
		{ID: "stable-2", Email: "user@example.com"},
		{ID: "", Email: "missing@example.com"},
	})
	if len(valid) != 1 || valid[0].Email != "user@example.com" || valid[0].ID != "stable-1" {
		t.Fatalf("valid entries = %#v", valid)
	}
	if summary.Conflicts != 2 || summary.Skipped != 1 {
		t.Fatalf("summary = %#v", summary)
	}
}
