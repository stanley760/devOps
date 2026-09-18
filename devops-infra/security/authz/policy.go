package authz

import "fmt"

func (a *CasbinAuthorizer) RebuildRolePolicies(role, domain string, permissions []string) error {
	enforcer, err := a.getEnforcer()
	if err != nil {
		return err
	}

	if _, err := enforcer.RemoveFilteredPolicy(0, role, domain); err != nil {
		return fmt.Errorf("remove role policies: %w", err)
	}
	if len(permissions) == 0 {
		return nil
	}

	rules := make([][]string, 0, len(permissions))
	for _, permission := range permissions {
		rules = append(rules, []string{role, domain, permission})
	}
	if _, err := enforcer.AddPolicies(rules); err != nil {
		return fmt.Errorf("add role policies: %w", err)
	}
	return nil
}

func (a *CasbinAuthorizer) RemoveRolePolicies(role, domain string) error {
	enforcer, err := a.getEnforcer()
	if err != nil {
		return err
	}

	if _, err := enforcer.RemoveFilteredPolicy(0, role, domain); err != nil {
		return fmt.Errorf("remove role policies: %w", err)
	}
	return nil
}

func (a *CasbinAuthorizer) RebuildUserGrouping(subject, domain string, roles []string) error {
	enforcer, err := a.getEnforcer()
	if err != nil {
		return err
	}

	if _, err := enforcer.RemoveFilteredNamedGroupingPolicy("g", 0, subject, "", domain); err != nil {
		return fmt.Errorf("remove user grouping: %w", err)
	}
	if len(roles) == 0 {
		return nil
	}

	rules := make([][]string, 0, len(roles))
	for _, role := range roles {
		rules = append(rules, []string{subject, role, domain})
	}
	if _, err := enforcer.AddNamedGroupingPolicies("g", rules); err != nil {
		return fmt.Errorf("add user grouping: %w", err)
	}
	return nil
}

func (a *CasbinAuthorizer) RemoveUserGrouping(subject, domain string) error {
	enforcer, err := a.getEnforcer()
	if err != nil {
		return err
	}

	if _, err := enforcer.RemoveFilteredNamedGroupingPolicy("g", 0, subject, "", domain); err != nil {
		return fmt.Errorf("remove user grouping: %w", err)
	}
	return nil
}

func (a *CasbinAuthorizer) RemoveRoleGrouping(role, domain string) error {
	enforcer, err := a.getEnforcer()
	if err != nil {
		return err
	}

	if _, err := enforcer.RemoveFilteredNamedGroupingPolicy("g", 1, role, domain); err != nil {
		return fmt.Errorf("remove role grouping: %w", err)
	}
	return nil
}

func (a *CasbinAuthorizer) AddPolicyIfAbsent(policyType string, values ...string) error {
	enforcer, err := a.getEnforcer()
	if err != nil {
		return err
	}

	switch policyType {
	case "p":
		has, err := enforcer.HasPolicy(toAny(values)...)
		if err != nil {
			return err
		}
		if !has {
			if _, err := enforcer.AddPolicy(toAny(values)...); err != nil {
				return err
			}
		}
		return nil
	case "g", "g2":
		has, err := enforcer.HasNamedGroupingPolicy(policyType, toAny(values)...)
		if err != nil {
			return err
		}
		if !has {
			if _, err := enforcer.AddNamedGroupingPolicy(policyType, toAny(values)...); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported policy type: %s", policyType)
	}
}

func toAny(values []string) []any {
	out := make([]any, len(values))
	for i, value := range values {
		out[i] = value
	}
	return out
}
