package main

import "testing"

func TestServiceProcessReadinessAndRetainedCredentials(t *testing.T) {
	units := []string{"netdata.service", "relay-a", "relay-b", "relay-c"}
	pids := map[string]string{"netdata.service": "1", "relay-a": "2", "relay-b": "3", "relay-c": "4"}
	clean := "Uid:\t987\t987\t987\t987\nGid:\t987\t987\t987\t987\nGroups:\t987\n"
	statuses := map[string]string{"1": clean, "2": clean, "3": clean, "4": clean}
	check := func(wantReady, wantError bool) {
		t.Helper()
		ready, err := processGroupsReady(statuses, pids, units, "987", "994")
		if ready != wantReady || (err != nil) != wantError {
			t.Fatalf("ready=%v err=%v; want ready=%v error=%v", ready, err, wantReady, wantError)
		}
	}
	check(true, false)
	// A Type=simple daemon is active before its own UID drop completes. This
	// is a bounded readiness wait, never evidence of revoked credentials yet.
	statuses["1"] = "Uid:\t0\t0\t0\t0\nGid:\t987\t987\t987\t987\nGroups:\t987\n"
	check(false, false)
	statuses["1"] = clean
	check(true, false)
	// Extra healthy plugins cannot conceal a missing required relay MainPID.
	statuses["5"], statuses["6"] = clean, clean
	delete(statuses, "4")
	check(false, false)
	statuses["4"] = clean
	delete(pids, "relay-c")
	check(false, false)
	pids["relay-c"] = "4"
	// Check every Netdata process, including helpers outside the four MainPIDs.
	statuses["6"] = clean + "Groups:\t987 994\n"
	check(false, true)
	// A retained real/effective/saved filesystem GID is equally privileged.
	statuses["6"] = "Uid:\t987\t987\t987\t987\nGid:\t987\t987\t994\t987\nGroups:\t987\n"
	check(false, true)
	statuses["6"] = clean
	// The deliberately separate privileged broker must not be mistaken for
	// Netdata merely because it uses Docker's supplementary group.
	statuses["7"] = "Uid:\t61234\t61234\t61234\t61234\nGid:\t61234\t61234\t61234\t61234\nGroups:\t994\n"
	check(true, false)
}
