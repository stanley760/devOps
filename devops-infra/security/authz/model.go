package authz

import "github.com/casbin/casbin/v3/model"

const tenantDomainModel = `
[request_definition]
r = sub, dom, obj

[policy_definition]
p = sub, dom, obj

[role_definition]
g = _, _, _
g2 = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = (g(r.sub, p.sub, r.dom) || g2(r.sub, p.sub)) && (r.dom == p.dom || p.dom == "*") && keyMatch(r.obj, p.obj)
`

func newModel() (model.Model, error) {
	return model.NewModelFromString(tenantDomainModel)
}
