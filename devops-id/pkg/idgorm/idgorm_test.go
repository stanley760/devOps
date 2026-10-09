package idgorm

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
)

type fakeGenerator struct {
	calls []string
}

type AuditEvent struct{}

type ExplicitTagModel struct{}

func (ExplicitTagModel) BizTag() string { return "custom_event" }

func (g *fakeGenerator) Next(_ context.Context, bizTag string) (int64, error) {
	g.calls = append(g.calls, bizTag)
	return int64(len(g.calls)), nil
}

func TestAssignSkipsExplicitID(t *testing.T) {
	generator := &fakeGenerator{}
	hook := New(generator, map[string]string{"t_demo": "demo"})
	id := int64(100)

	if err := hook.Assign(&gorm.DB{}, &id); err != nil {
		t.Fatalf("Assign() error = %v", err)
	}
	if id != 100 {
		t.Fatalf("id = %d, want 100", id)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %v, want none", generator.calls)
	}
}

func TestAssignRejectsMissingGeneratorAndUnknownTable(t *testing.T) {
	hook := New(nil, map[string]string{"t_demo": "demo"})
	id := int64(0)

	if err := hook.Assign(&gorm.DB{}, &id); !errors.Is(err, ErrGeneratorNotInitialized) {
		t.Fatalf("Assign() error = %v, want ErrGeneratorNotInitialized", err)
	}

	hook.SetGenerator(&fakeGenerator{})
	if err := hook.Assign(&gorm.DB{Statement: &gorm.Statement{Table: "t_unknown"}}, &id); err == nil {
		t.Fatal("Assign() with unknown table should fail")
	}
	if id != 0 {
		t.Fatalf("id = %d, want 0", id)
	}
}

func TestAssignUsesTableBizTag(t *testing.T) {
	generator := &fakeGenerator{}
	hook := New(nil, map[string]string{"t_demo": "demo"})
	hook.SetGenerator(generator)
	id := int64(0)
	tx := &gorm.DB{Statement: &gorm.Statement{
		Context: context.Background(),
		Table:   "t_demo",
	}}

	if err := hook.Assign(tx, &id); err != nil {
		t.Fatalf("Assign() error = %v", err)
	}
	if id != 1 {
		t.Fatalf("id = %d, want 1", id)
	}
	if len(generator.calls) != 1 || generator.calls[0] != "demo" {
		t.Fatalf("generator calls = %v, want [demo]", generator.calls)
	}
}

func TestAssignDerivesBizTagFromModel(t *testing.T) {
	generator := &fakeGenerator{}
	hook := New(generator, nil)
	id := int64(0)
	tx := &gorm.DB{Statement: &gorm.Statement{
		Context: context.Background(),
		Table:   "t_audit_events",
		Model:   &AuditEvent{},
	}}

	if err := hook.Assign(tx, &id); err != nil {
		t.Fatalf("Assign() error = %v", err)
	}
	if len(generator.calls) != 1 || generator.calls[0] != "audit_event" {
		t.Fatalf("generator calls = %v, want [audit_event]", generator.calls)
	}
}

func TestAssignUsesExplicitModelBizTag(t *testing.T) {
	generator := &fakeGenerator{}
	hook := New(generator, nil)
	id := int64(0)
	tx := &gorm.DB{Statement: &gorm.Statement{
		Context: context.Background(),
		Table:   "t_custom_events",
		Model:   &ExplicitTagModel{},
	}}

	if err := hook.Assign(tx, &id); err != nil {
		t.Fatalf("Assign() error = %v", err)
	}
	if len(generator.calls) != 1 || generator.calls[0] != "custom_event" {
		t.Fatalf("generator calls = %v, want [custom_event]", generator.calls)
	}
}

func TestBizTagOfUsesModelOverride(t *testing.T) {
	hook := New(nil, nil)
	tag, err := hook.BizTagOf(ExplicitTagModel{})
	if err != nil {
		t.Fatalf("BizTagOf() error = %v", err)
	}
	if tag != "custom_event" {
		t.Fatalf("BizTagOf() = %q, want custom_event", tag)
	}
}

func TestTagsReturnsUniqueSortedTags(t *testing.T) {
	hook := New(nil, map[string]string{
		"t_users":   "user",
		"t_roles":   "role",
		"t_tenants": "tenant",
	})

	got := hook.Tags()
	want := []string{"role", "tenant", "user"}
	if len(got) != len(want) {
		t.Fatalf("Tags() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Tags() = %v, want %v", got, want)
		}
	}
}
