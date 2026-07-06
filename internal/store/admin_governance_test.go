package store

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestNormalizeAdminUserInputKeepsUsernameOptional(t *testing.T) {
	input, err := normalizeAdminUserInput(AdminUserInput{
		Email:        "Ops.User@example.com",
		PasswordHash: "hashed-password",
	}, true)
	if err != nil {
		t.Fatal(err)
	}

	if input.Username != "" {
		t.Fatalf("Username = %q, want empty", input.Username)
	}
	if input.Name != "ops.user" {
		t.Fatalf("Name = %q, want email prefix", input.Name)
	}
}

func TestNormalizeAdminUserInputNormalizesExplicitUsername(t *testing.T) {
	input, err := normalizeAdminUserInput(AdminUserInput{
		Email:        "ops@example.com",
		Username:     " Ops.Admin_01! ",
		PasswordHash: "hashed-password",
	}, true)
	if err != nil {
		t.Fatal(err)
	}

	if input.Username != "ops.admin_01" {
		t.Fatalf("Username = %q, want ops.admin_01", input.Username)
	}
}

func TestAdminUserStatsUsesAggregateQueryBeyondListLimit(t *testing.T) {
	query := &fakeStatsQuery{values: []int{301, 300, 1, 0, 0, 250, 1, 2, 298, 3, 299, 2, 0, 0}}

	stats, err := adminUserStats(context.Background(), query, "true", nil)
	if err != nil {
		t.Fatal(err)
	}

	if stats["total"] != 301 {
		t.Fatalf("total stats = %d, want 301", stats["total"])
	}
	if stats["free"] != 298 || stats["superVip"] != 3 {
		t.Fatalf("plan stats = free %d superVip %d, want 298/3", stats["free"], stats["superVip"])
	}
	assertAggregateStatsQuery(t, query.query)
}

func TestAdminOrgStatsUsesAggregateQueryBeyondListLimit(t *testing.T) {
	query := &fakeStatsQuery{values: []int{350, 345, 3, 1, 1, 2, 348, 0, 0}}

	stats, err := adminOrgStats(context.Background(), query, "true and o.status=$2", []any{DefaultOrgID, "active"})
	if err != nil {
		t.Fatal(err)
	}

	if stats["total"] != 350 {
		t.Fatalf("total stats = %d, want 350", stats["total"])
	}
	if len(query.args) != 2 || query.args[0] != DefaultOrgID || query.args[1] != "active" {
		t.Fatalf("stats args = %#v, want default org id and active filter", query.args)
	}
	assertAggregateStatsQuery(t, query.query)
}

func assertAggregateStatsQuery(t *testing.T, query string) {
	t.Helper()
	normalized := strings.ToLower(query)
	if !strings.Contains(normalized, "count(*) filter") {
		t.Fatalf("stats query should use aggregate filters, got %s", query)
	}
	if strings.Contains(normalized, "limit 300") {
		t.Fatalf("stats query must not inherit list limit, got %s", query)
	}
}

type fakeStatsQuery struct {
	query  string
	args   []any
	values []int
}

func (q *fakeStatsQuery) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	q.query = query
	q.args = args
	return fakeStatsRow{values: q.values}
}

type fakeStatsRow struct {
	values []int
}

func (r fakeStatsRow) Scan(dest ...any) error {
	for index := range dest {
		value := 0
		if index < len(r.values) {
			value = r.values[index]
		}
		ptr, ok := dest[index].(*int)
		if !ok {
			continue
		}
		*ptr = value
	}
	return nil
}
