package restoreswitch

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The model uses digest tokens for immutable fixture contents. Two tokens
// represent the new DB before and after its management update. Production
// identity, native holds, DB commits and durable publication are separate.
type fixtureIdentity [sha256.Size]byte

type recoveryPlan struct {
	Version                                     int
	ID                                          string
	OldDB, OldImage, NewDB, UpdatedDB, NewImage fixtureIdentity
}

type recoveryCompletion struct {
	Version    int
	ID         string
	PlanDigest fixtureIdentity
}

type recoveryMove struct{ from, to string }

const (
	planName     = "records/plan.json"
	completeName = "records/complete.json"
	liveDB       = "live/db"
	liveImage    = "live/image"
	stagedDB     = "staged/db"
	stagedImage  = "staged/image"
	retiredDB    = "retired/db"
	retiredImage = "retired/image"
)

var errRecoveryStop = errors.New("recovery evidence is uncertain")

func fixtureToken(s string) fixtureIdentity { return sha256.Sum256([]byte(s)) }

func readModelFile(root, name string) ([]byte, bool, error) {
	name = filepath.Join(root, name)
	info, err := os.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, errRecoveryStop
	}
	body, err := os.ReadFile(name)
	return body, true, err
}

// decideRecovery models startup under an already acquired exclusive lock.
// It reads all candidates before returning any moves and receives no checkpoint.
func decideRecovery(root string) (string, []recoveryMove, error) {
	return decideRecoveryWithIdentity(root, func(root, name string) (fixtureIdentity, bool, error) {
		body, found, err := readModelFile(root, name)
		return sha256.Sum256(body), found, err
	})
}

func decideRecoveryWithIdentity(root string, observe func(string, string) (fixtureIdentity, bool, error)) (string, []recoveryMove, error) {
	records, err := os.ReadDir(filepath.Join(root, "records"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", nil, errRecoveryStop
	}
	for _, entry := range records {
		name := strings.ToLower(entry.Name())
		if strings.HasPrefix(name, ".manga-update-backup-") && strings.HasSuffix(name, ".partial") {
			return "", nil, errRecoveryStop
		}
	}
	for _, name := range []string{planName + ".tmp", completeName + ".tmp"} {
		if _, found, err := readModelFile(root, name); err != nil || found {
			return "", nil, errRecoveryStop
		}
	}
	raw, planFound, err := readModelFile(root, planName)
	if err != nil {
		return "", nil, errRecoveryStop
	}
	done, doneFound, err := readModelFile(root, completeName)
	if err != nil {
		return "", nil, errRecoveryStop
	}
	if !planFound {
		if doneFound {
			return "", nil, errRecoveryStop
		}
		// The fixture has one known work area. An orphaned retirement or
		// evidence of installation without a plan is ambiguous.
		for _, name := range []string{retiredDB, retiredImage} {
			if _, found, err := readModelFile(root, name); found || err != nil {
				return "", nil, errRecoveryStop
			}
		}
		for _, name := range []string{liveDB, liveImage} {
			if _, found, err := readModelFile(root, name); !found || err != nil {
				return "", nil, errRecoveryStop
			}
		}
		// This outcome reports only that no recovery record was found.
		// Without a plan we cannot identify the live files or authorize startup.
		return "no plan", nil, nil
	}
	var p recoveryPlan
	if json.Unmarshal(raw, &p) != nil || p.Version != 1 || p.ID == "" {
		return "", nil, errRecoveryStop
	}
	if doneFound {
		var c recoveryCompletion
		if json.Unmarshal(done, &c) != nil || c.Version != 1 || c.ID != p.ID || c.PlanDigest != sha256.Sum256(raw) {
			return "", nil, errRecoveryStop
		}
	}

	var evacuate, rollback []recoveryMove
	for _, x := range []struct {
		live, staged, retired string
		old, new, updated     fixtureIdentity
	}{
		{liveDB, stagedDB, retiredDB, p.OldDB, p.NewDB, p.UpdatedDB},
		{liveImage, stagedImage, retiredImage, p.OldImage, p.NewImage, p.NewImage},
	} {
		liveID, hasLive, a := observe(root, x.live)
		stagedID, hasStaged, b := observe(root, x.staged)
		retiredID, hasRetired, c := observe(root, x.retired)
		if errors.Join(a, b, c) != nil || x.old == x.new || x.old == x.updated {
			return "", nil, errRecoveryStop
		}
		if doneFound {
			if !hasLive || liveID != x.updated || hasStaged || !hasRetired || retiredID != x.old {
				return "", nil, errRecoveryStop
			}
			continue
		}
		switch {
		case hasLive && liveID == x.old && !hasRetired && hasStaged && (stagedID == x.new || stagedID == x.updated):
			// This part never moved, or a prior recovery already restored it.
		case !hasLive && hasRetired && retiredID == x.old && hasStaged && (stagedID == x.new || stagedID == x.updated):
			rollback = append(rollback, recoveryMove{x.retired, x.live})
		case hasLive && liveID == x.updated && hasRetired && retiredID == x.old && !hasStaged:
			evacuate = append(evacuate, recoveryMove{x.live, x.staged})
			rollback = append(rollback, recoveryMove{x.retired, x.live})
		default:
			return "", nil, errRecoveryStop
		}
	}
	if doneFound {
		return "keep new", nil, nil
	}
	return "restore old", append(evacuate, rollback...), nil
}

var recoveryFixturePlan = recoveryPlan{
	Version: 1, ID: "synthetic-restore",
	OldDB: fixtureToken("old-db"), OldImage: fixtureToken("old-image"),
	NewDB: fixtureToken("new-db"), UpdatedDB: fixtureToken("updated-db"), NewImage: fixtureToken("new-image"),
}

func recoveryFixture(t *testing.T, steps int) string {
	t.Helper()
	root := t.TempDir()
	write(t, root, liveDB, "old-db")
	write(t, root, liveImage, "old-image")
	write(t, root, stagedDB, "new-db")
	write(t, root, stagedImage, "new-image")
	if err := os.Mkdir(filepath.Join(root, "retired"), 0700); err != nil {
		t.Fatal(err)
	}
	plan, err := json.Marshal(recoveryFixturePlan)
	if err != nil {
		t.Fatal(err)
	}
	done, err := json.Marshal(recoveryCompletion{1, recoveryFixturePlan.ID, sha256.Sum256(plan)})
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []func(){
		func() { write(t, root, planName, string(plan)) },
		func() { move(t, root, liveDB, retiredDB) },
		func() { move(t, root, liveImage, retiredImage) },
		func() { move(t, root, stagedImage, liveImage) },
		func() { write(t, root, stagedDB, "updated-db") },
		func() { move(t, root, stagedDB, liveDB) },
		func() { write(t, root, completeName, string(done)) },
	}[:steps] {
		step()
	}
	return root
}

func TestRecoveryFromRecordsAndFiles(t *testing.T) {
	for steps := 0; steps <= 7; steps++ {
		t.Run(fmt.Sprint(steps), func(t *testing.T) {
			root := recoveryFixture(t, steps)
			want := "restore old"
			if steps == 0 {
				want = "no plan"
			}
			if steps == 7 {
				want = "keep new"
			}
			// Restart after each operation in the recovery plan and decide again.
			for restarts := 0; ; restarts++ {
				outcome, moves, err := decideRecovery(root)
				if outcome != want || err != nil {
					t.Fatal(outcome, moves, err)
				}
				if len(moves) == 0 {
					break
				}
				if restarts >= 4 {
					t.Fatal("recovery did not converge")
				}
				move(t, root, moves[0].from, moves[0].to)
			}
			if want == "keep new" {
				content(t, root, liveDB, "updated-db")
				content(t, root, liveImage, "new-image")
				content(t, root, retiredDB, "old-db")
				content(t, root, retiredImage, "old-image")
			} else {
				content(t, root, liveDB, "old-db")
				content(t, root, liveImage, "old-image")
				content(t, root, stagedImage, "new-image")
				wantDB := "new-db"
				if steps >= 5 {
					wantDB = "updated-db"
				}
				content(t, root, stagedDB, wantDB)
			}
		})
	}
}

func TestRecoveryStopsOnUncertainEvidence(t *testing.T) {
	plan, err := json.Marshal(recoveryFixturePlan)
	if err != nil {
		t.Fatal(err)
	}
	wrongID, err := json.Marshal(recoveryCompletion{1, "other", sha256.Sum256(plan)})
	if err != nil {
		t.Fatal(err)
	}
	wrongDigest, err := json.Marshal(recoveryCompletion{1, recoveryFixturePlan.ID, fixtureToken("other plan")})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, path, body string
		steps            int
	}{
		{"partial plan", planName + ".tmp", "{", 0},
		{"invalid plan", planName, "{", 1},
		{"partial completion", completeName + ".tmp", "{", 6},
		{"invalid completion", completeName, "{", 6},
		{"different restore ID", completeName, string(wrongID), 6},
		{"different plan digest", completeName, string(wrongDigest), 6},
		{"wrong final identity", liveImage, "unexpected", 7},
		{"rollback destination occupied", liveDB, "unexpected", 4},
		{"staging destination occupied", stagedImage, "unexpected", 4},
		{"incomplete DB update", stagedDB, "partially-updated", 4},
		{"unidentified retired data", retiredDB, "unexpected", 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := recoveryFixture(t, tc.steps)
			write(t, root, tc.path, tc.body)
			if _, moves, err := decideRecovery(root); !errors.Is(err, errRecoveryStop) || len(moves) != 0 {
				t.Fatal(moves, err)
			}
			content(t, root, tc.path, tc.body)
		})
	}
}

func TestRecoveryWithoutPlanAndMissingLiveFileStops(t *testing.T) {
	root := recoveryFixture(t, 0)
	move(t, root, liveDB, retiredDB+".unexpected")
	if _, moves, err := decideRecovery(root); !errors.Is(err, errRecoveryStop) || len(moves) != 0 {
		t.Fatal(moves, err)
	}
}
