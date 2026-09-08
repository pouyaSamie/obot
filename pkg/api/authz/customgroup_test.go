package authz

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/obot-platform/obot/apiclient/types"
	"k8s.io/apiserver/pkg/authentication/user"
)

func TestCustomGroupAuthorization(t *testing.T) {
	authorizer := NewAuthorizer(nil, nil, nil, false, nil, nil, nil, false)
	admin := &user.DefaultInfo{UID: "admin", Groups: types.RoleAdmin.Groups()}
	basic := &user.DefaultInfo{UID: "basic", Groups: types.RoleBasic.Groups()}

	for _, test := range []struct {
		name    string
		method  string
		path    string
		user    user.Info
		allowed bool
	}{
		{"admin can create", http.MethodPost, "/api/custom-groups", admin, true},
		{"admin can read a custom group", http.MethodGet, "/api/custom-groups/custom%2Fgroup-id", admin, true},
		{"admin can update a custom group", http.MethodPatch, "/api/custom-groups/custom%2Fgroup-id", admin, true},
		{"admin can update group members", http.MethodPut, "/api/custom-groups/custom%2Fgroup-id/members", admin, true},
		{"admin can delete a custom group", http.MethodDelete, "/api/custom-groups/custom%2Fgroup-id", admin, true},
		{"basic user cannot read a custom group", http.MethodGet, "/api/custom-groups/custom%2Fgroup-id", basic, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(test.method, test.path, nil)
			if got := authorizer.Authorize(req, test.user); got != test.allowed {
				t.Fatalf("Authorize(%s %s) = %v, want %v", test.method, test.path, got, test.allowed)
			}
		})
	}
}