// Command seed fills a running erp/costing with the seed data for people (be-assembly-standard's seed and test data
// conventions). Most of the costing data is not written here: erp/costing values erp/inventory's movements as they come
// (the inventory, purchase, sales and finance seeds make them), so this command sets what only costing owns and then
// waits for the valuation to catch up:
//
//   - access, by the authorization admin: the role maintain (the seed user: the policies and the valuation), read in
//     every legal entity (test.finance.lead) and in CN01 (test.accountant.cn);
//   - policies: CN01 values by moving average, ES01 and US01 by FIFO. A method can change only before a movement of the
//     legal entity is valued (PO01), so `make seed` runs this command with the argument `policies` before the
//     inventory seed; a legal entity already valued keeps its method, with a warning;
//   - without the argument, after the policies, it waits until no valuation scope is waiting and the number of scopes
//     stays the same for a while, fails when one is stopped, and prints the scopes per legal entity, those below zero
//     (IS02: the seeded receipts are valued, but the opening receipts have no source document, so they take the policy
//     price, 0 when nothing is valued yet) and the layers.
//
// The grants are keyed changes and a policy already at its method is not written again, so a second run changes
// nothing. The legal entities come from mdm/org (its seed runs first: make seed).
//
//	SEED_IAM_URL (http://localhost:18200)    the identity member's development exchange
//	SEED_AUTHZ_URL (http://localhost:18223)  the authorization member's effect point
//	SEED_ADMIN (test.alice)                  the authorization admin (the install claim)
//	SEED_USER (test.bob), SEED_PASSWORD      the user that writes the data; the test accounts' password
//	SEED_COSTING_WAIT (10m)                  how long to wait for the valuation to catch up
//	ERP_COSTING_URL (http://localhost:18229), MDM_ORG_URL (http://localhost:18083)
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	sdkauthz "github.com/brickKit/be-sdk-go/v3/authz"
	"github.com/brickKit/be-sdk-go/v3/seed"
)

const prefix = "「本地测试」"

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "seed erp/costing: "+format+"\n", a...)
	os.Exit(1)
}

func check(err error) {
	if err != nil {
		fail("%v", err)
	}
}

type client struct{ seed.Client }

func (c client) must(method, path string, body any, header ...string) map[string]any {
	out, err := c.Must(method, path, body, header...)
	check(err)
	return out
}

// segment grants an operation in the given legal entities (all when none).
func segment(resource, op string, legalEntities ...string) sdkauthz.Segment {
	s := seed.Segment(resource, op)
	le := sdkauthz.Atom{Kind: "all"}
	if len(legalEntities) > 0 {
		le = sdkauthz.Atom{Kind: "in", Values: legalEntities}
	}
	s.Atoms = map[string]sdkauthz.Atom{"legal_entity": le}
	return s
}

func reads(legalEntities ...string) []sdkauthz.Segment {
	return []sdkauthz.Segment{segment("erp.costing.policy", "erp.costing.policy.read", legalEntities...),
		segment("erp.costing.valuation", "erp.costing.valuation.read", legalEntities...)}
}

// legalEntities are mdm/org's seeded legal entities by code.
func legalEntities(org client) map[string]string {
	out := map[string]string{}
	for _, e := range org.must("GET", "/api/org/legal-entities?limit=200", nil)["legal_entities"].([]any) {
		e := e.(map[string]any)
		out[e["code"].(string)] = e["id"].(string)
	}
	for _, code := range []string{"CN01", "ES01", "US01"} {
		if out[code] == "" {
			fail("mdm/org has no legal entity %s: run its seed first", code)
		}
	}
	return out
}

var methods = map[string]string{"CN01": "moving_average", "ES01": "fifo", "US01": "fifo"}

func main() {
	policiesOnly := len(os.Args) > 1 && os.Args[1] == "policies"
	base := env("ERP_COSTING_URL", "http://localhost:18229")
	if !seed.Ready(base) {
		fail("erp/costing at %s is not ready", base)
	}
	pw := os.Getenv("SEED_PASSWORD")
	if pw == "" {
		fail("SEED_PASSWORD is not set (the test accounts' password in Casdoor)")
	}
	wait, err := time.ParseDuration(env("SEED_COSTING_WAIT", "10m"))
	check(err)
	iam, user, admin := env("SEED_IAM_URL", "http://localhost:18200"), env("SEED_USER", "test.bob"), env("SEED_ADMIN", "test.alice")
	accounts, err := seed.Accounts(iam, pw, admin, user, "test.finance.lead", "test.accountant.cn")
	check(err)
	t := accounts[user].Token
	les := legalEntities(client{seed.NewClient(env("MDM_ORG_URL", "http://localhost:18083"), t)})
	a, err := seed.NewAuthz(env("SEED_AUTHZ_URL", "http://localhost:18223"), accounts[admin].Token, "erp/costing")
	check(err)
	_, err = a.GrantRoles([]seed.RoleSpec{
		{Key: "maintain", Name: prefix + "成本维护", To: []string{user}, Segments: append(reads(),
			segment("erp.costing.policy", "erp.costing.policy.manage"))},
		{Key: "read", Name: prefix + "成本只读", To: []string{"test.finance.lead"}, Segments: reads()},
		{Key: "read-cn01", Name: prefix + "成本只读（CN01）", To: []string{"test.accountant.cn"}, Segments: reads(les["CN01"])},
	}, accounts)
	check(err)

	c := client{seed.NewClient(base, t)}
	set := 0
	for _, code := range []string{"CN01", "ES01", "US01"} {
		if policy(c, les[code], code) {
			set++
		}
	}
	fmt.Printf("seed erp/costing: %d policies set\n", set)
	if policiesOnly {
		return
	}
	settle(c, les, wait)
}

// policy sets a legal entity's method unless it has it (set); a legal entity already valued keeps its method. The first
// read waits for the seed user's grant to reach erp/costing.
func policy(c client, le, code string) (set bool) {
	st, p, err := c.UntilGranted(30*time.Second, "GET", "/api/costing/policies/"+le, nil)
	check(err)
	if st != 200 {
		fail("policy of %s: %d %v", code, st, p)
	}
	if p["method"] == methods[code] {
		return false
	}
	st, out, err := c.Do("PUT", "/api/costing/policies/"+le, map[string]any{"method": methods[code], "version": p["version"]})
	check(err)
	switch {
	case st == 200:
		return true
	case strings.Contains(fmt.Sprint(out), "POLICY_IN_USE"):
		fmt.Printf("seed erp/costing: %s is valued already, it keeps %v (PO01)\n", code, p["method"])
		return false
	}
	fail("policy of %s: %d %v", code, st, out)
	return false
}

type tally struct{ scopes, waiting, stopped, negative int }

// count reads every scope of a legal entity.
func count(c client, le string) (n tally, stopped []string) {
	cursor := ""
	for {
		page := c.must("GET", "/api/costing/scopes?limit=200&legal_entity_id="+le+"&cursor="+cursor, nil)
		for _, s := range page["scopes"].([]any) {
			s := s.(map[string]any)
			n.scopes++
			switch s["state"] {
			case "waiting_gap", "waiting_basis":
				n.waiting++
			case "stopped":
				n.stopped++
				stopped = append(stopped, fmt.Sprintf("%v: %v", s["product_id"], s["stopped_reason"]))
			}
			if q, _ := s["quantity"].(string); strings.HasPrefix(q, "-") {
				n.negative++
			}
		}
		cursor, _ = page["next_cursor"].(string)
		if cursor == "" {
			return n, stopped
		}
	}
}

// settle waits until no scope waits and the number of scopes holds for three reads in a row, then prints the tallies;
// a stopped scope fails the seed (a movement this version cannot value).
func settle(c client, les map[string]string, wait time.Duration) {
	codes := make([]string, 0, len(methods))
	for code := range methods {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	deadline, last, same := time.Now().Add(wait), -1, 0
	for {
		total, waiting := 0, 0
		tallies := map[string]tally{}
		for _, code := range codes {
			n, stopped := count(c, les[code])
			if len(stopped) > 0 {
				fail("%s has stopped valuation scopes: %s", code, strings.Join(stopped, "; "))
			}
			tallies[code] = n
			total, waiting = total+n.scopes, waiting+n.waiting
		}
		if waiting == 0 && total == last {
			same++
		} else {
			same = 0
		}
		if same >= 2 {
			for _, code := range codes {
				n := tallies[code]
				layers := len(c.must("GET", "/api/costing/layers?limit=200&legal_entity_id="+les[code], nil)["layers"].([]any))
				fmt.Printf("seed erp/costing: %s (%s) %d scopes, %d below zero, layers on the first page %d\n",
					code, methods[code], n.scopes, n.negative, layers)
			}
			return
		}
		last = total
		if time.Now().After(deadline) {
			fail("the valuation has not caught up in %s: %d scopes, %d waiting", wait, total, waiting)
		}
		time.Sleep(10 * time.Second)
	}
}
