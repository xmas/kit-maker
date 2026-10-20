package authz_test

import (
	"context"
	"slices"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	"github.com/docker/kit-maker/internal/authz"
	"github.com/docker/kit-maker/internal/ent"
	"github.com/docker/kit-maker/internal/teams"
	"github.com/docker/kit-maker/internal/testutil"
)

func TestBuiltInRoles(t *testing.T) {
	for _, r := range authz.BuiltInRoles {
		for _, k := range r.Permissions {
			p, ok := authz.Lookup(k)
			if !ok {
				t.Fatalf("%s: unknown permission %s", r.Key, k)
			}
			if r.Scope == authz.ScopeTeam && p.Scope != authz.ScopeTeam {
				t.Errorf("team role %s carries deployment permission %s", r.Key, k)
			}
		}
	}
	viewer, _ := authz.LookupRole(authz.RoleViewer)
	maint, _ := authz.LookupRole(authz.RoleMaintainer)
	if slices.Contains(viewer.Permissions, authz.BlueprintEdit) {
		t.Error("viewer must not edit blueprints")
	}
	for _, k := range []authz.Key{authz.BlueprintPublish, authz.BlueprintPromote, "approval.decide.widening"} {
		if !slices.Contains(maint.Permissions, k) {
			t.Errorf("maintainer missing %s", k)
		}
	}
	admin, _ := authz.LookupRole(authz.RoleAdmin)
	if !slices.Contains(admin.Permissions, authz.TeamCreate) {
		t.Error("admins create teams")
	}
	for _, r := range authz.BuiltInRoles {
		if r.Key != authz.RoleOwner && r.Key != authz.RoleAdmin && slices.Contains(r.Permissions, authz.TeamCreate) {
			t.Errorf("only admins create teams, but %s can", r.Key)
		}
	}
}

func TestSubject(t *testing.T) {
	ctx := context.Background()
	db := testutil.StartPostgres(t)
	c := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db.SQL)))
	ts := &teams.Service{Ent: c}
	az := &authz.Authorizer{Ent: c}

	eng := must(ts.Create(ctx, "Engineering", "", ""))
	plat := must(ts.Create(ctx, "Platform", "", "engineering"))
	pay := must(ts.Create(ctx, "Payments", "", "platform"))
	other := must(ts.Create(ctx, "Other", "", ""))

	sam := must(c.User.Create().SetEmail("sam@acme.com").Save(ctx))
	jo := must(c.User.Create().SetEmail("jo@acme.com").Save(ctx))
	boss := must(c.User.Create().SetEmail("boss@acme.com").Save(ctx))

	if err := ts.AddMember(ctx, "payments", sam.Email, authz.RoleMaintainer); err != nil {
		t.Fatal(err)
	}
	if err := ts.AddMember(ctx, "other", jo.Email, "owner"); err == nil {
		t.Fatal("owner is a deployment role, not a team role")
	}
	if err := ts.AssignDeploymentRole(ctx, boss.Email, authz.RoleOwner); err != nil {
		t.Fatal(err)
	}
	if err := ts.SetParent(ctx, "engineering", "payments"); err == nil {
		t.Fatal("cycles must be refused")
	}

	s := must(az.Load(ctx, sam.ID))
	t.Run("direct team role", func(t *testing.T) {
		if !s.Can(authz.BlueprintPublish, pay.ID) || !s.Can(authz.BlueprintEdit, pay.ID) {
			t.Error("maintainer of payments should publish and edit there")
		}
		if s.Can(authz.TeamCreate, "") {
			t.Error("team roles never grant team.create")
		}
	})
	t.Run("parents grant view only", func(t *testing.T) {
		for _, team := range []string{plat.ID, eng.ID} {
			if !s.Can(authz.BlueprintView, team) {
				t.Errorf("child member should view parent %s", team)
			}
			if s.Can(authz.BlueprintEdit, team) {
				t.Errorf("child member must not edit parent %s", team)
			}
		}
		if s.Can(authz.BlueprintView, other.ID) {
			t.Error("unrelated team must be invisible")
		}
		ids, all := s.TeamsWith(authz.BlueprintView)
		if all || len(ids) != 3 {
			t.Errorf("TeamsWith(blueprint.view) = %v all=%v, want payments, platform, engineering", ids, all)
		}
		ids, _ = s.TeamsWith(authz.BlueprintEdit)
		if len(ids) != 1 || ids[0] != pay.ID {
			t.Errorf("TeamsWith(blueprint.edit) = %v, want only payments", ids)
		}
	})
	t.Run("exclusion wins over every source", func(t *testing.T) {
		if err := c.TeamMembership.Create().SetTeamID(pay.ID).SetUserID(sam.ID).SetRole(authz.RoleViewer).
			SetIsExcluded(true).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		s2 := must(az.Load(ctx, sam.ID))
		if s2.Can(authz.BlueprintView, pay.ID) || s2.Can(authz.BlueprintView, plat.ID) {
			t.Error("excluded member keeps access")
		}
	})
	t.Run("unknown role keys grant nothing", func(t *testing.T) {
		if err := c.TeamMembership.Create().SetTeamID(other.ID).SetUserID(jo.ID).SetRole("release-manager").Exec(ctx); err != nil {
			t.Fatal(err)
		}
		if must(az.Load(ctx, jo.ID)).CanAnywhere(authz.BlueprintView) {
			t.Error("unknown role granted access")
		}
	})
	t.Run("owner can do everything everywhere", func(t *testing.T) {
		b := must(az.Load(ctx, boss.ID))
		if !b.Can(authz.TeamCreate, "") || !b.Can(authz.BlueprintPromote, other.ID) {
			t.Error("owner lacks permissions")
		}
		if _, all := b.TeamsWith(authz.BlueprintView); !all {
			t.Error("owner should see all teams")
		}
	})
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
