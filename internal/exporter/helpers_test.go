package exporter

import (
	"encoding/json"
	"testing"

	"gitlab.com/torn-city/go-torn-client/client"
)

func TestStatusState(t *testing.T) {
	t.Parallel()

	var state client.UserStatus_State
	if err := state.FromUserStatusStateEnum(client.UserStatusStateEnumHospital); err != nil {
		t.Fatal(err)
	}
	got := statusState(client.UserStatus{State: state, Color: "red"})
	if got != "Hospital" {
		t.Fatalf("got %q", got)
	}
}

func TestApplyJobRegular(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(map[string]any{
		"type":     "job",
		"name":     "Casino",
		"position": "Dealer",
	})
	if err != nil {
		t.Fatal(err)
	}
	var job client.UserJobResponse_Job
	if err := job.UnmarshalJSON(raw); err != nil {
		t.Fatal(err)
	}
	snap := &accountSnapshot{}
	applyJob(snap, &client.UserJobResponse{Job: &job})
	if snap.jobType != "job" || snap.jobName != "Casino" || snap.jobPosition != "Dealer" {
		t.Fatalf("job fields = %+v", snap)
	}
}

func TestBoolAndOptional(t *testing.T) {
	t.Parallel()
	if boolFloat(true) != 1 || boolFloat(false) != 0 {
		t.Fatal("boolFloat")
	}
	v := int32(42)
	if optionalFloat(&v) != 42 || optionalFloat(nil) != 0 {
		t.Fatal("optionalFloat")
	}
}
