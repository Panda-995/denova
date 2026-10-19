package permission_test

import (
	"testing"

	"github.com/alfredxw/denova/agent/tool/permission"
	"github.com/alfredxw/denova/agent/tool/permission/permissiontest"
)

func TestCodingPolicyContract(t *testing.T) {
	permissiontest.RunPolicyContract(t, func(testing.TB) permission.PermissionPolicy {
		policy, err := permission.CodingWithRules(permission.MemoryRules())
		if err != nil {
			t.Fatal(err)
		}
		return policy
	})
}
